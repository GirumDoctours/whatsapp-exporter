package main

import (
	_ "embed"
	"encoding/json"
	"fmt"
	"regexp"
	"sort"
	"strconv"
	"strings"
)

//go:embed redact_dictionary.json
var embeddedDictionary []byte

var (
	tagPattern = regexp.MustCompile(`\{\{[A-Z0-9_]+\}\}`)
	dictionary = mustLoadDictionary(embeddedDictionary)
)

type tagKind int

const (
	tagPerson tagKind = iota
	tagEntity
	tagStandalone
)

type tagDictionary struct {
	Version            int                 `json:"version"`
	BaseRoles          []string            `json:"base_roles"`
	Relations          []string            `json:"relations"`
	PersonAttributes   []string            `json:"person_attributes"`
	Entities           map[string][]string `json:"entities"`
	Standalone         []string            `json:"standalone"`
	FallbackRoles      []string            `json:"fallback_roles"`
	FallbackEntities   map[string][]string `json:"fallback_entities"`
	FallbackStandalone []string            `json:"fallback_standalone"`

	bases              map[string]bool
	relations          map[string]bool
	attributes         []string
	standalone         map[string]bool
	fallbackRoles      map[string]bool
	fallbackStandalone map[string]bool
	entityFields       map[string]map[string]bool
	fallbackEntitySet  map[string]bool
}

type parsedTag struct {
	Name      string
	Kind      tagKind
	Base      string
	Relation  string
	Number    int
	Attribute string
	Fallback  bool
}

func mustLoadDictionary(data []byte) *tagDictionary {
	d, err := loadDictionary(data)
	if err != nil {
		panic(err)
	}
	return d
}

func loadDictionary(data []byte) (*tagDictionary, error) {
	var d tagDictionary
	if err := json.Unmarshal(data, &d); err != nil {
		return nil, fmt.Errorf("reading the tag dictionary: %w", err)
	}
	d.index()
	return &d, nil
}

func setOf(values ...[]string) map[string]bool {
	set := map[string]bool{}
	for _, list := range values {
		for _, v := range list {
			set[v] = true
		}
	}
	return set
}

func (d *tagDictionary) index() {
	d.bases = setOf(d.BaseRoles)
	d.relations = setOf(d.Relations)
	d.standalone = setOf(d.Standalone)
	d.fallbackRoles = setOf(d.FallbackRoles)
	d.fallbackStandalone = setOf(d.FallbackStandalone)
	d.attributes = append([]string(nil), d.PersonAttributes...)
	sort.Slice(d.attributes, func(i, j int) bool { return len(d.attributes[i]) > len(d.attributes[j]) })
	d.entityFields = map[string]map[string]bool{}
	d.fallbackEntitySet = map[string]bool{}
	for prefix, fields := range d.Entities {
		d.entityFields[prefix] = setOf(fields)
	}
	for prefix, fields := range d.FallbackEntities {
		d.entityFields[prefix] = setOf(fields)
		d.fallbackEntitySet[prefix] = true
	}
}

func stripBraces(tag string) string {
	return strings.TrimSuffix(strings.TrimPrefix(tag, "{{"), "}}")
}

func splitNumber(name string) (string, int) {
	cut := strings.LastIndexByte(name, '_')
	if cut <= 0 {
		return name, 0
	}
	n, err := strconv.Atoi(name[cut+1:])
	if err != nil || n < 2 || n > 9999 {
		return name, 0
	}
	return name[:cut], n
}

func (d *tagDictionary) role(name string) (base, relation string, fallback, ok bool) {
	if d.bases[name] {
		return name, "", false, true
	}
	if d.fallbackRoles[name] {
		return name, "", true, true
	}
	for candidate := range d.bases {
		if rel, found := strings.CutPrefix(name, candidate+"_"); found && d.relations[rel] {
			return candidate, rel, false, true
		}
	}
	return "", "", false, false
}

func (d *tagDictionary) parse(tag string) (parsedTag, bool) {
	name := stripBraces(tag)
	if name == "" {
		return parsedTag{}, false
	}
	if base, n := splitNumber(name); d.standalone[base] || d.fallbackStandalone[base] {
		return parsedTag{Name: name, Kind: tagStandalone, Attribute: base, Number: n, Fallback: d.fallbackStandalone[base]}, true
	}
	for _, attr := range d.attributes {
		rest, found := strings.CutSuffix(name, "_"+attr)
		if !found || rest == "" {
			continue
		}
		roleName, n := splitNumber(rest)
		if base, rel, fallback, ok := d.role(roleName); ok {
			return parsedTag{Name: name, Kind: tagPerson, Base: base, Relation: rel, Number: n, Attribute: attr, Fallback: fallback}, true
		}
	}
	for prefix, fields := range d.entityFields {
		if !strings.HasPrefix(name, prefix) {
			continue
		}
		for field := range fields {
			rest, found := strings.CutSuffix(name, "_"+field)
			if !found {
				continue
			}
			if entity, n := splitNumber(rest); entity == prefix {
				return parsedTag{Name: name, Kind: tagEntity, Base: prefix, Number: n, Attribute: field, Fallback: d.fallbackEntitySet[prefix]}, true
			}
		}
	}
	return parsedTag{}, false
}

func (d *tagDictionary) valid(tag string) bool {
	_, ok := d.parse(tag)
	return ok
}

func (p parsedTag) role() string {
	if p.Relation == "" {
		return p.Base
	}
	return p.Base + "_" + p.Relation
}

func (p parsedTag) String() string {
	return "{{" + p.Name + "}}"
}

func (p parsedTag) withNumber(n int) parsedTag {
	number := ""
	if n >= 2 {
		number = "_" + strconv.Itoa(n)
	}
	switch p.Kind {
	case tagStandalone:
		p.Name = p.Attribute + number
	default:
		p.Name = p.role() + number + "_" + p.Attribute
	}
	p.Number = n
	return p
}

func roleTag(role, attribute string) string {
	return "{{" + role + "_" + attribute + "}}"
}

func (d *tagDictionary) fallbackFor(tag string) string {
	name := stripBraces(tag)
	for _, attr := range d.attributes {
		if strings.HasSuffix(name, attr) {
			return roleTag("PERSON", attr)
		}
	}
	switch {
	case strings.Contains(name, "LINK") || strings.Contains(name, "URL"):
		return "{{LINK}}"
	case strings.Contains(name, "DATE"):
		return "{{DATE}}"
	case strings.Contains(name, "PHONE"):
		return "{{PHONE_NUMBER}}"
	case strings.Contains(name, "EMAIL"):
		return "{{EMAIL_ADDRESS}}"
	case strings.Contains(name, "ADDRESS"):
		return "{{ADDRESS}}"
	case strings.HasSuffix(name, "_NAME"):
		return "{{ORGANIZATION_NAME}}"
	case strings.Contains(name, "NUMBER") || strings.Contains(name, "_ID") || strings.Contains(name, "REF"):
		return "{{ID_NUMBER}}"
	}
	return "{{OTHER_PII}}"
}

func (d *tagDictionary) promptText() string {
	var b strings.Builder
	b.WriteString("TAG GRAMMAR\n")
	b.WriteString("People: {{ROLE_ATTRIBUTE}} where ROLE is a base role, or a base role + \"_\" + relation (relation of that base person).\n")
	b.WriteString("A second distinct person with the same role in one chat gets a number after the role: {{PATIENT_FRIEND_2_FIRST_NAME}}, {{DOCTOR_2_LAST_NAME}}.\n")
	b.WriteString("Organisations and places: {{ENTITY_FIELD}}, numbered the same way: {{HOTEL_2_NAME}}.\n")
	b.WriteString("Standalone tags may take a number at the end: {{BOOKING_REF_2}}.\n")
	b.WriteString("Fallbacks only when nothing else fits: PERSON as a role, ORGANIZATION as an entity, and the fallback standalone tags.\n\n")
	b.WriteString("BASE ROLES: " + strings.Join(d.BaseRoles, ", ") + "\n")
	b.WriteString("RELATIONS: " + strings.Join(d.Relations, ", ") + "\n")
	b.WriteString("PERSON ATTRIBUTES: " + strings.Join(d.PersonAttributes, ", ") + "\n")
	b.WriteString("ENTITIES:\n")
	for _, prefix := range sortedKeys(d.Entities) {
		b.WriteString("  " + prefix + ": " + strings.Join(d.Entities[prefix], ", ") + "\n")
	}
	b.WriteString("STANDALONE: " + strings.Join(d.Standalone, ", ") + "\n")
	b.WriteString("FALLBACK ROLE: " + strings.Join(d.FallbackRoles, ", ") + "\n")
	for _, prefix := range sortedKeys(d.FallbackEntities) {
		b.WriteString("FALLBACK ENTITY " + prefix + ": " + strings.Join(d.FallbackEntities[prefix], ", ") + "\n")
	}
	b.WriteString("FALLBACK STANDALONE: " + strings.Join(d.FallbackStandalone, ", ") + "\n")
	return b.String()
}

func sortedKeys[V any](m map[string]V) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}

type dictionaryAdditions struct {
	BaseRoles        []string            `json:"base_roles"`
	Relations        []string            `json:"relations"`
	PersonAttributes []string            `json:"person_attributes"`
	Entities         map[string][]string `json:"entities"`
	Standalone       []string            `json:"standalone"`
}

var upperSnake = regexp.MustCompile(`^[A-Z][A-Z0-9]*(_[A-Z0-9]+)*$`)

func appendNew(list []string, additions []string) ([]string, []string) {
	seen := setOf(list)
	var added []string
	for _, a := range additions {
		a = strings.TrimSpace(a)
		if !upperSnake.MatchString(a) || seen[a] || len(a) > 40 {
			continue
		}
		seen[a] = true
		list = append(list, a)
		added = append(added, a)
	}
	return list, added
}

func (d *tagDictionary) merge(add dictionaryAdditions) []string {
	var changes []string
	note := func(kind string, added []string) {
		for _, a := range added {
			changes = append(changes, kind+": "+a)
		}
	}
	var added []string
	d.BaseRoles, added = appendNew(d.BaseRoles, add.BaseRoles)
	note("base role", added)
	d.Relations, added = appendNew(d.Relations, add.Relations)
	note("relation", added)
	d.PersonAttributes, added = appendNew(d.PersonAttributes, add.PersonAttributes)
	note("person attribute", added)
	d.Standalone, added = appendNew(d.Standalone, add.Standalone)
	note("standalone", added)
	for prefix, fields := range add.Entities {
		if !upperSnake.MatchString(prefix) {
			continue
		}
		d.Entities[prefix], added = appendNew(d.Entities[prefix], fields)
		if len(d.Entities[prefix]) == 0 {
			delete(d.Entities, prefix)
		}
		note("entity "+prefix, added)
	}
	d.index()
	return changes
}

func (d *tagDictionary) marshal() ([]byte, error) {
	d.Version++
	return json.MarshalIndent(d, "", "  ")
}
