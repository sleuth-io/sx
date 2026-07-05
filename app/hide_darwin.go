//go:build darwin

package main

/*
#cgo CFLAGS: -x objective-c
#cgo LDFLAGS: -framework Cocoa

#import <Cocoa/Cocoa.h>

static void SxHideOtherApplications(void) {
	dispatch_async(dispatch_get_main_queue(), ^{
		[NSApp hideOtherApplications:nil];
	});
}

static void SxUnhideAllApplications(void) {
	dispatch_async(dispatch_get_main_queue(), ^{
		[NSApp unhideAllApplications:nil];
	});
}
*/
import "C"

// hideOtherApplications and unhideAllApplications back the standard macOS
// app-menu items (⌥⌘H / Show All). Wails v2 exposes Hide for the app
// itself but not these two, so they go straight to AppKit.
func hideOtherApplications() { C.SxHideOtherApplications() }

func unhideAllApplications() { C.SxUnhideAllApplications() }
