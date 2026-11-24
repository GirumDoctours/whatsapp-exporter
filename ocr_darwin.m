#import <Foundation/Foundation.h>
#import <Vision/Vision.h>
#import <ImageIO/ImageIO.h>
#include <stdlib.h>
#include <string.h>

static NSDictionary *boxOf(CGRect box) {
	return @{
		@"x": @(box.origin.x),
		@"y": @(1.0 - box.origin.y - box.size.height),
		@"w": @(box.size.width),
		@"h": @(box.size.height),
	};
}

char *ocr_recognize(const char *path) {
	@autoreleasepool {
		NSURL *url = [NSURL fileURLWithPath:[NSString stringWithUTF8String:path]];
		CGImageSourceRef source = CGImageSourceCreateWithURL((__bridge CFURLRef)url, NULL);
		if (source == NULL) {
			return NULL;
		}
		CGImageRef image = CGImageSourceCreateImageAtIndex(source, 0, NULL);
		CFRelease(source);
		if (image == NULL) {
			return NULL;
		}
		VNRecognizeTextRequest *text = [[VNRecognizeTextRequest alloc] init];
		text.recognitionLevel = VNRequestTextRecognitionLevelAccurate;
		text.usesLanguageCorrection = NO;
		if (@available(macOS 13.0, *)) {
			text.automaticallyDetectsLanguage = YES;
		}
		VNDetectBarcodesRequest *barcodes = [[VNDetectBarcodesRequest alloc] init];
		VNDetectFaceRectanglesRequest *faces = [[VNDetectFaceRectanglesRequest alloc] init];
		VNImageRequestHandler *handler = [[VNImageRequestHandler alloc] initWithCGImage:image options:@{}];
		NSError *error = nil;
		BOOL ok = [handler performRequests:@[text, barcodes, faces] error:&error];
		CGImageRelease(image);
		if (!ok) {
			return NULL;
		}
		NSMutableArray *lines = [NSMutableArray array];
		for (VNRecognizedTextObservation *observation in text.results) {
			VNRecognizedText *top = [[observation topCandidates:1] firstObject];
			if (top == nil) {
				continue;
			}
			NSMutableDictionary *line = [boxOf(observation.boundingBox) mutableCopy];
			line[@"t"] = top.string;
			line[@"c"] = @(top.confidence);
			[lines addObject:line];
		}
		NSMutableArray *codes = [NSMutableArray array];
		for (VNBarcodeObservation *observation in barcodes.results) {
			NSMutableDictionary *code = [boxOf(observation.boundingBox) mutableCopy];
			code[@"k"] = observation.symbology ?: @"";
			[codes addObject:code];
		}
		NSMutableArray *found = [NSMutableArray array];
		for (VNFaceObservation *observation in faces.results) {
			[found addObject:boxOf(observation.boundingBox)];
		}
		NSData *json = [NSJSONSerialization dataWithJSONObject:@{@"lines": lines, @"barcodes": codes, @"faces": found} options:0 error:nil];
		if (json == nil) {
			return NULL;
		}
		char *out = malloc(json.length + 1);
		memcpy(out, json.bytes, json.length);
		out[json.length] = 0;
		return out;
	}
}
