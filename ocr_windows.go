//go:build windows

package main

import (
	"bufio"
	"encoding/base64"
	"encoding/binary"
	"encoding/json"
	"errors"
	"io"
	"os/exec"
	"strings"
	"syscall"
	"time"
	"unicode/utf16"
)

const (
	ocrHelpers        = 2
	ocrRequestTimeout = 45 * time.Second
	createNoWindow    = 0x08000000
)

const ocrScript = `$ErrorActionPreference = 'Stop'
[Console]::InputEncoding = [Text.Encoding]::UTF8
[Console]::OutputEncoding = [Text.Encoding]::UTF8
Add-Type -AssemblyName System.Runtime.WindowsRuntime
$null = [Windows.Storage.StorageFile, Windows.Storage, ContentType = WindowsRuntime]
$null = [Windows.Media.Ocr.OcrEngine, Windows.Foundation, ContentType = WindowsRuntime]
$null = [Windows.Graphics.Imaging.BitmapDecoder, Windows.Graphics, ContentType = WindowsRuntime]
$asTask = ([System.WindowsRuntimeSystemExtensions].GetMethods() | Where-Object { $_.Name -eq 'AsTask' -and $_.GetParameters().Count -eq 1 -and $_.GetParameters()[0].ParameterType.Name -eq 'IAsyncOperation` + "`" + `1' })[0]
function Await($op, [Type]$type) { $task = $asTask.MakeGenericMethod($type).Invoke($null, @($op)); $task.Wait(-1) | Out-Null; $task.Result }
$detector = $null
try {
  $null = [Windows.Media.FaceAnalysis.FaceDetector, Windows.Media.FaceAnalysis, ContentType = WindowsRuntime]
  if ([Windows.Media.FaceAnalysis.FaceDetector]::IsSupported) {
    $detector = Await ([Windows.Media.FaceAnalysis.FaceDetector]::CreateAsync()) ([Windows.Media.FaceAnalysis.FaceDetector])
  }
} catch { $detector = $null }
$engine = [Windows.Media.Ocr.OcrEngine]::TryCreateFromUserProfileLanguages()
if ($null -eq $engine) { [Console]::Out.WriteLine('{"error":"no text recognition language is installed"}'); exit 1 }
[Console]::Out.WriteLine('{"ready":true}')
while ($null -ne ($path = [Console]::In.ReadLine())) {
  try {
    $file = Await ([Windows.Storage.StorageFile]::GetFileFromPathAsync($path)) ([Windows.Storage.StorageFile])
    $stream = Await ($file.OpenAsync([Windows.Storage.FileAccessMode]::Read)) ([Windows.Storage.Streams.IRandomAccessStream])
    $decoder = Await ([Windows.Graphics.Imaging.BitmapDecoder]::CreateAsync($stream)) ([Windows.Graphics.Imaging.BitmapDecoder])
    $w = [double]$decoder.PixelWidth; $h = [double]$decoder.PixelHeight
    $scale = [Math]::Min(1.0, [Windows.Media.Ocr.OcrEngine]::MaxImageDimension / [Math]::Max($w, $h))
    $transform = New-Object Windows.Graphics.Imaging.BitmapTransform
    $transform.ScaledWidth = [uint32][Math]::Max(1, [Math]::Floor($w * $scale))
    $transform.ScaledHeight = [uint32][Math]::Max(1, [Math]::Floor($h * $scale))
    $bitmap = Await ($decoder.GetSoftwareBitmapAsync([Windows.Graphics.Imaging.BitmapPixelFormat]::Bgra8, [Windows.Graphics.Imaging.BitmapAlphaMode]::Premultiplied, $transform, [Windows.Graphics.Imaging.ExifOrientationMode]::IgnoreExifOrientation, [Windows.Graphics.Imaging.ColorManagementMode]::DoNotColorManage)) ([Windows.Graphics.Imaging.SoftwareBitmap])
    $result = Await ($engine.RecognizeAsync($bitmap)) ([Windows.Media.Ocr.OcrResult])
    $sw = [double]$transform.ScaledWidth; $sh = [double]$transform.ScaledHeight
    $lines = New-Object System.Collections.ArrayList
    foreach ($line in $result.Lines) {
      $minX = [double]::MaxValue; $minY = [double]::MaxValue; $maxX = 0.0; $maxY = 0.0
      foreach ($word in $line.Words) {
        $r = $word.BoundingRect
        $minX = [Math]::Min($minX, $r.X); $minY = [Math]::Min($minY, $r.Y)
        $maxX = [Math]::Max($maxX, $r.X + $r.Width); $maxY = [Math]::Max($maxY, $r.Y + $r.Height)
      }
      [void]$lines.Add(@{ t = $line.Text; x = $minX / $sw; y = $minY / $sh; w = ($maxX - $minX) / $sw; h = ($maxY - $minY) / $sh })
    }
    $faces = New-Object System.Collections.ArrayList
    if ($null -ne $detector) {
      try {
        $gray = [Windows.Graphics.Imaging.SoftwareBitmap]::Convert($bitmap, [Windows.Graphics.Imaging.BitmapPixelFormat]::Gray8)
        $found = Await ($detector.DetectFacesAsync($gray)) ([System.Collections.Generic.IList[Windows.Media.FaceAnalysis.DetectedFace]])
        foreach ($face in $found) {
          $r = $face.FaceBox
          [void]$faces.Add(@{ x = $r.X / $sw; y = $r.Y / $sh; w = $r.Width / $sw; h = $r.Height / $sh })
        }
      } catch { }
    }
    $stream.Dispose()
    [Console]::Out.WriteLine((ConvertTo-Json -Compress -Depth 4 @{ lines = $lines.ToArray(); faces = $faces.ToArray() }))
  } catch {
    [Console]::Out.WriteLine((ConvertTo-Json -Compress @{ error = $_.Exception.Message }))
  }
}
`

type ocrHelper struct {
	cmd    *exec.Cmd
	stdin  io.WriteCloser
	stdout *bufio.Reader
}

type windowsRecognizer struct {
	pool chan *ocrHelper
}

type windowsReply struct {
	Ready bool         `json:"ready"`
	Error string       `json:"error"`
	Lines []visionLine `json:"lines"`
	Faces []visionLine `json:"faces"`
}

type visionLine struct {
	T string  `json:"t"`
	X float64 `json:"x"`
	Y float64 `json:"y"`
	W float64 `json:"w"`
	H float64 `json:"h"`
}

func encodedScript() string {
	units := utf16.Encode([]rune(ocrScript))
	buf := make([]byte, len(units)*2)
	for i, u := range units {
		binary.LittleEndian.PutUint16(buf[i*2:], u)
	}
	return base64.StdEncoding.EncodeToString(buf)
}

func startOCRHelper() (*ocrHelper, error) {
	cmd := exec.Command("powershell.exe", "-NoProfile", "-NonInteractive", "-ExecutionPolicy", "Bypass", "-EncodedCommand", encodedScript())
	cmd.SysProcAttr = &syscall.SysProcAttr{HideWindow: true, CreationFlags: createNoWindow}
	stdin, err := cmd.StdinPipe()
	if err != nil {
		return nil, err
	}
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return nil, err
	}
	if err := cmd.Start(); err != nil {
		return nil, err
	}
	h := &ocrHelper{cmd: cmd, stdin: stdin, stdout: bufio.NewReader(stdout)}
	reply, err := h.readReply(ocrRequestTimeout)
	if err != nil {
		h.stop()
		return nil, err
	}
	if !reply.Ready {
		h.stop()
		return nil, errors.New(firstNonEmpty(reply.Error, "text recognition didn't start"))
	}
	return h, nil
}

func (h *ocrHelper) readReply(timeout time.Duration) (windowsReply, error) {
	type result struct {
		line string
		err  error
	}
	ch := make(chan result, 1)
	go func() {
		line, err := h.stdout.ReadString('\n')
		ch <- result{line, err}
	}()
	select {
	case r := <-ch:
		if r.err != nil {
			return windowsReply{}, r.err
		}
		var reply windowsReply
		if err := json.Unmarshal([]byte(strings.TrimSpace(strings.TrimPrefix(r.line, "\ufeff"))), &reply); err != nil {
			return windowsReply{}, err
		}
		return reply, nil
	case <-time.After(timeout):
		return windowsReply{}, errors.New("text recognition timed out")
	}
}

func (h *ocrHelper) stop() {
	_ = h.stdin.Close()
	if h.cmd.Process != nil {
		_ = h.cmd.Process.Kill()
	}
	_ = h.cmd.Wait()
}

func newTextRecognizer() (textRecognizer, error) {
	r := &windowsRecognizer{pool: make(chan *ocrHelper, ocrHelpers)}
	for i := 0; i < ocrHelpers; i++ {
		h, err := startOCRHelper()
		if err != nil {
			logOCR.Warnf("Windows text recognition helper %d didn't start: %v", i+1, err)
			if i == 0 {
				return nil, errors.New("text recognition isn't available: " + err.Error())
			}
			break
		}
		r.pool <- h
	}
	return r, nil
}

func (r *windowsRecognizer) recognize(path string) (ocrResult, error) {
	var h *ocrHelper
	select {
	case h = <-r.pool:
	case <-time.After(2 * ocrRequestTimeout):
		return ocrResult{}, errors.New("text recognition is busy or stopped")
	}
	if _, err := io.WriteString(h.stdin, path+"\n"); err != nil {
		h.stop()
		r.replace()
		return ocrResult{}, err
	}
	reply, err := h.readReply(ocrRequestTimeout)
	if err != nil {
		logOCR.Warnf("Text recognition helper stopped responding, restarting it: %v", err)
		h.stop()
		r.replace()
		return ocrResult{}, err
	}
	r.pool <- h
	if reply.Error != "" {
		return ocrResult{}, errors.New(reply.Error)
	}
	var res ocrResult
	for _, l := range reply.Lines {
		res.Lines = append(res.Lines, ocrLine{Text: l.T, Conf: 1, X: l.X, Y: l.Y, W: l.W, H: l.H})
	}
	for _, f := range reply.Faces {
		res.Faces = append(res.Faces, ocrLine{X: f.X, Y: f.Y, W: f.W, H: f.H})
	}
	return res, nil
}

func (r *windowsRecognizer) replace() {
	go func() {
		h, err := startOCRHelper()
		if err != nil {
			return
		}
		r.pool <- h
	}()
}

func (r *windowsRecognizer) close() {
	for {
		select {
		case h := <-r.pool:
			h.stop()
		default:
			return
		}
	}
}
