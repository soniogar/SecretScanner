package jobs

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/deepfence/SecretScanner/core"
	"github.com/deepfence/SecretScanner/scan"
)

func writeSecretScanStatus(status, scan_id, scan_message string) {
	var secretScanLogDoc = make(map[string]interface{})
	secretScanLogDoc["scan_id"] = scan_id
	secretScanLogDoc["scan_status"] = status
	secretScanLogDoc["scan_message"] = scan_message

	byteJson, err := json.Marshal(secretScanLogDoc)
	if err != nil {
		fmt.Println("Error marshalling json for secret-logs-status: ", err)
		return
	}

	err = writeScanDataToFile(string(byteJson), scanStatusFilename)
	if err != nil {
		fmt.Println("Error in sending data to secret-logs-status to mark in progress:" + err.Error())
		return
	}
}

func writeScanDataToFile(secretScanMsg string, filename string) error {
	err := os.MkdirAll(filepath.Dir(filename), 0755)
	f, err := os.OpenFile(filename, os.O_APPEND|os.O_WRONLY|os.O_CREATE, 0600)
	if err != nil {
		return err
	}

	defer f.Close()

	secretScanMsg = strings.Replace(secretScanMsg, "\n", " ", -1)
	if _, err = f.WriteString(secretScanMsg + "\n"); err != nil {
		return err
	}
	return nil
}

func getDfInstallDir() string {
	installDir, exists := os.LookupEnv("DF_INSTALL_DIR")
	if exists {
		return installDir
	} else {
		return ""
	}
}

func StartStatusReporter(ctx context.Context, scanCtx *scan.ScanContext) chan error {
	res := make(chan error)
	startScanJob()
	scan_id := scanCtx.ScanID
	opts := core.GetSession().Options

	//If we don't get any active status back within threshold,
	//we consider the scan job as dead
	threshold := *opts.InactiveThreshold
	go func() {
		defer stopScanJob()
		ticker := time.NewTicker(1 * time.Second)
		var err error
		ts := time.Now()
		core.GetSession().Log.Error("SecretScan StatusReporter started, scan_id: %s", scan_id)
	loop:
		for {
			select {
			case err = <-res:
				break loop
			case <-ctx.Done():
				err = ctx.Err()
				break loop
			case <-scanCtx.ScanStatusChan:
				ts = time.Now()
			case <-ticker.C:
				//We perform the check once per 30 seconds
				if scanCtx.Stopped.Load() == true {
					core.GetSession().Log.Error("Scanner job stopped, scan_id: %s", scan_id)
					break loop
				}

				elapsed := int(time.Since(ts).Seconds())
				if elapsed > threshold {
					err = fmt.Errorf("Scan job aborted due to inactivity")
					core.GetSession().Log.Error("Scanner job aborted due to inactivity, scan_id: %s", scan_id)
					scanCtx.Aborted.Store(true)
					break loop
				} else {
					writeSecretScanStatus("IN_PROGRESS", scan_id, "")
				}
			}
		}

		if scanCtx.Stopped.Load() == true {
			writeSecretScanStatus("CANCELLED", scan_id, "Scan stopped by user")
		} else if err != nil {
			writeSecretScanStatus("ERROR", scan_id, err.Error())
		} else {
			writeSecretScanStatus("COMPLETE", scan_id, "")
		}

		core.GetSession().Log.Error("SecretScan StatusReporter stopped, scan_id: %s", scan_id)
	}()
	return res
}
