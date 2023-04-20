package common

import (
	"fmt"
	"github.com/platinasystems/scsi"
	log "github.com/sirupsen/logrus"
	"io/fs"
	"io/ioutil"
	"os"
	"path/filepath"
	"regexp"
	"strings"
)

const blockPath = "/sys/class/block/"

func GetDiskByID(diskId string) (disk string, err error) {
	var (
		regexNVMeDev = regexp.MustCompile(`^nvme\d+n\d+$`)
		targetDisk   string
		blockNames   map[string]struct{}
	)
	diskId = extractDeviceID(diskId)
	if blockNames, err = getDrivesNames(); err != nil {
		return
	}
	for blockDeviceName := range blockNames {
		var relPath string
		path := filepath.Join(blockPath, blockDeviceName, "device")
		if regexNVMeDev.MatchString(blockDeviceName) {
			relPath = filepath.Join(blockDeviceName, "wwid")
		} else {
			relPath = "wwid"
		}
		naaId := readFileContent(path, relPath)
		// t10.ATA     SanDisk SD8SMAT128G1122                 181277400692
		if splits := strings.Split(naaId, " "); len(splits) > 2 {
			var ids []string
			if ids, _, err = GetIsciId(fmt.Sprintf("/dev/%s", blockDeviceName)); err == nil {
				for _, id := range ids {
					if diskId == id {
						targetDisk = blockDeviceName
						goto End
					}
				}
			}
		} else if s := strings.Split(naaId, "."); len(s) > 1 {
			if diskId == s[1] {
				targetDisk = blockDeviceName
				goto End
			}
		}
	}
End:
	if targetDisk == "" {
		err = fmt.Errorf("destination disk with id %s not found", diskId)
		return
	}
	disk = fmt.Sprintf("/dev/%s", targetDisk)
	return
}

func GetDiskBySN(diskSN string) (disk string, err error) {
	var (
		sn         string
		targetDisk string
		blockNames map[string]struct{}
	)
	if blockNames, err = getDrivesNames(); err != nil {
		return
	}
	for blockDeviceName := range blockNames {
		path := filepath.Join(blockPath, blockDeviceName, "device")
		if sn = readFileContent(path, "vpd_pg80"); sn != "" {
			word := regexp.MustCompile("\\w+")
			sn = strings.Join(word.FindAllString(sn, -1), "")
		} else {
			// NVMe device
			sn = readFileContent(path, "serial")
		}
		if sn != "" && diskSN == sn {
			targetDisk = blockDeviceName
			break
		}
	}
	if targetDisk == "" {
		err = fmt.Errorf("destination disk with sn %s not found", diskSN)
		return
	}
	disk = fmt.Sprintf("/dev/%s", targetDisk)

	return
}

func extractDeviceID(diskId string) string {
	// NVMe device
	if sd := strings.Split(diskId, "."); len(sd) > 1 {
		diskId = sd[1]
		if sd = strings.Split(diskId, "-"); len(sd) > 1 {
			diskId = sd[0]
		}
	}
	if sd := strings.Split(diskId, "-"); len(sd) > 1 {
		diskId = sd[1]
		if strings.Contains(diskId, "0x") {
			diskId = strings.Replace(diskId, "0x", "", 1)
		}
	}
	return diskId
}

func getDrivesNames() (DrivesNames map[string]struct{}, err error) {
	var blockDevices []fs.FileInfo
	blockDevices, err = ioutil.ReadDir(blockPath)
	if err != nil {
		err = fmt.Errorf("cannot read %v: %v", blockPath, err)
		return
	}
	DrivesNames = make(map[string]struct{})
	for _, f := range blockDevices {
		path := filepath.Join(blockPath, f.Name(), "device")
		if fi, e := os.Stat(path); os.IsNotExist(e) || !fi.IsDir() {
			// block device is not a drive
			continue
		}
		DrivesNames[f.Name()] = struct{}{}
	}
	return
}

func readFileContent(paths ...string) string {
	path := filepath.Join(paths...)
	b, _ := ioutil.ReadFile(path)
	return strings.TrimSpace(string(b))
}

func GetIsciId(device string) (ids []string, sn string, err error) {
	dev, e := scsi.Open(device)
	if e != nil {
		err = e
		return
	}
	defer dev.Close()

	pages, e := dev.VPDs()
	if e != nil {
		err = e
		return
	}

	for _, pc := range pages {
		switch pc {
		case scsi.Page80:
			if inq, e := dev.SerialNumber(); e != nil {
				log.Warn(e)
			} else {
				sn = inq.String()
			}
		case scsi.Page83:
			if dscs, e := dev.IDs(); e != nil {
				log.Warn(e)
			} else if len(dscs) > 0 {
				fmt.Println("ID:")
				for _, dsc := range dscs {
					l := strings.Fields(dsc.String())
					if len(l) == 1 {
						ids = append(ids, dsc.String())
					}
				}
			}
		}
	}
	return
}
