package main

import (
	"fmt"
	"github.com/tinkerbell/hub/actions/common"
	"os"
	"strconv"

	log "github.com/sirupsen/logrus"
	"github.com/tinkerbell/hub/actions/image2disk/v1/pkg/image"
)

func main() {
	fmt.Printf("IMAGE2DISK - Cloud image streamer\n------------------------\n")
	disk := os.Getenv("DEST_DISK")
	diskId := os.Getenv("DEST_DISK_ID")
	diskSN := os.Getenv("DEST_DISK_SN")
	img := os.Getenv("IMG_URL")
	compressedEnv := os.Getenv("COMPRESSED")
	var err error
	if diskId != "" {
		if disk, err = common.GetDiskByID(diskId); err != nil {
			log.Fatal(err)
			return
		}
	} else if diskSN != "" {
		if disk, err = common.GetDiskBySN(diskSN); err != nil {
			log.Fatal(err)
			return
		}
	}
	// We can ignore the error and default compressed to false.
	cmp, _ := strconv.ParseBool(compressedEnv)

	// Write the image to disk
	err = image.Write(img, disk, cmp)
	if err != nil {
		log.Fatal(err)
	}
	log.Infof("Successfully written [%s] to [%s]", img, disk)
}
