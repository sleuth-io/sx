//go:build !darwin

package main

// Hide Others / Show All are macOS conventions with no Windows or Linux
// equivalent; the menu items only exist on darwin, so these never run.
func hideOtherApplications() {}

func unhideAllApplications() {}
