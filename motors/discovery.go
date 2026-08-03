package motors

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// DiscoverMotorBusPath finds the USB serial device for the motorbus.
func DiscoverMotorBusPath() (string, error) {
	dir := "/dev/serial/by-id"
	listing, err := os.ReadDir(dir)
	if err != nil {
		return "", err
	}
	for _, pattern := range []string{"usb-1a86_USB_Single_Serial", "USB_Single_Serial", "usb-"} {
		var found []os.DirEntry
		for _, entry := range listing {
			if strings.Contains(entry.Name(), pattern) {
				found = append(found, entry)
			}
		}
		if len(found) > 1 {
			return "", fmt.Errorf("unable to locate unique motorbus; multiple devices found for pattern: %s", pattern)
		} else if len(found) == 1 {
			return filepath.Join(dir, found[0].Name()), nil
		}
	}
	return "", errors.New("unable to locate motorbus USB serial device")
}
