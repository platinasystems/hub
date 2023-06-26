package common

import (
	"encoding/json"
	"fmt"
	"github.com/platinasystems/go-common/v2/base"
	log2 "github.com/platinasystems/go-common/v2/logs"
	"github.com/platinasystems/go-common/v2/process"
	utility "github.com/platinasystems/go-common/v2/utilities"
	"github.com/platinasystems/go-common/v2/utils"
	"github.com/platinasystems/scsi"
	"github.com/platinasystems/tiles/pccserver/models"
	"github.com/platinasystems/tiles/systemCollector/smart"
	log "github.com/sirupsen/logrus"
	"github.com/spf13/afero"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"strings"
)

const blockPath = "/sys/class/block/"

var (
	ps       process.Service
	fsRef    afero.Fs
	selector *driveSelector
)

type driveSelector struct {
	smartStatGatherer *smart.SmartctlGatherer
	allDrives         []*models.Drive
}

func init() {
	log2.InitWithDefault(nil)

	ps = &process.OSProcessService{}
	fsRef = afero.NewOsFs()

	selector = &driveSelector{
		smartStatGatherer: smart.NewSmartctlGatherer(ps, fsRef),
	}
}

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
	var blockDevices []os.DirEntry
	blockDevices, err = os.ReadDir(blockPath)
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
	b, _ := os.ReadFile(path)
	return strings.TrimSpace(string(b))
}

func GetIsciId(device string) (ids []string, sn string, err error) {
	dev, e := scsi.Open(device)
	if e != nil {
		err = e
		return
	}
	defer func() { _ = dev.Close() }()

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

// GetDiskByLogicalID returns device path for a given drive specified with a logical composite identifier
func GetDiskByLogicalID(logicalDriveIDJSON string) (disk string, err error) {

	driveIn := make([]*models.LogicalDriveID, 0)
	err = json.Unmarshal([]byte(logicalDriveIDJSON), &driveIn)
	if utils.IsNotNull(err) {
		return
	}

	paths, ch := selector.getCompositeDrivePath(driveIn)
	if utils.IsNull(ch) {
		if len(paths) > 0 {
			log2.AuctaLogger.Infof("drive composed by %d has paths: %q", len(driveIn), paths)
		} else {
			log2.AuctaLogger.Infof("drive composed by %d not found", len(driveIn))
		}
	} else {
		log2.AuctaLogger.Infof(ch.Message())
	}

	return
}

func (selector *driveSelector) getCompositeDrivePath(in []*models.LogicalDriveID) (paths []string, ch base.CodeHolder) {
	if utils.IsNull(in) {
		return
	}

	drives, err := selector.smartStatGatherer.Scan()
	if utils.IsNotNull(err) {
		ch = base.BuildCodeHolder(err, fmt.Sprintf("smartctl error: %s", err.Error()), http.StatusInternalServerError)
		return
	}
	selector.allDrives = drives

	paths = make([]string, 0)

	var path string
	for _, singleInput := range in {
		path, ch = selector.getDrivePath(singleInput)
		if utils.IsNotNull(ch) {
			paths = nil
			return
		}
		log2.AuctaLogger.Infof("drive %s has path: %q", singleInput.String(), path)
		paths = append(paths, path)
	}
	return
}

func (selector *driveSelector) getDrivePath(in *models.LogicalDriveID) (path string, ch base.CodeHolder) {
	if utils.IsNull(in) || len(selector.allDrives) == 0 {
		return
	}

	for _, singleInput := range in.PhysicalDriveIDs {

		for _, drive := range selector.allDrives {

			if utility.StringIsNotBlank(singleInput.WWID) {
				if len(drive.Addresses) > 0 {

					log2.AuctaLogger.Infof("comparing %q and list %q", singleInput.WWID, drive.Addresses)
					if utils.Contains(drive.Addresses, strings.ToLower(singleInput.WWID)) {
						log2.AuctaLogger.Info("matched a SAS address")
						path = fmt.Sprintf("/dev/%s", drive.Name)
						return
					}

				} else {
					wwid := drive.WWID
					log2.AuctaLogger.Infof("comparing %q and %q", singleInput.WWID, wwid)
					// maybe WWID could be fetched from /sys/block/sd*/device/wwid too, before trying the partial match
					if strings.EqualFold(singleInput.WWID, wwid) || partialWWIDMatch(wwid, singleInput.WWID) {
						path = fmt.Sprintf("/dev/%s", drive.Name)
						return
					}
				}
			}

			// wwid and serial are not mutually exclusive: first match is OK
			if utility.StringIsNotBlank(singleInput.Serial) {
				// looking for a drive by serial number
				serialNumber := drive.SerialNumber
				log2.AuctaLogger.Infof("comparing %q and %q", singleInput.Serial, serialNumber)
				if strings.EqualFold(singleInput.Serial, serialNumber) {
					path = fmt.Sprintf("/dev/%s", drive.Name)
					return
				}
			}
		}
	}

	return
}

func partialWWIDMatch(wwn string, wwid string) bool {
	log2.AuctaLogger.Infof("comparing %q and %q", wwid[:len(wwid)-1], wwn[:len(wwn)-1])
	return len(wwn) > 0 && len(wwid) > 0 && strings.EqualFold(wwid[:len(wwid)-1], wwn[:len(wwn)-1])
}
