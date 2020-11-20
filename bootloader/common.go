package bootloader

import "regexp"

var RegRootUUID = regexp.MustCompile(`root=UUID=[0-9a-fA-F\-]+`)
