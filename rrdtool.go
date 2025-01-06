package main

import (
	"fmt"
	"log"
	"os/exec"
	"time"
)

func rrdupdate(addr, prefix string, val float64) error {
	db := "enphase.rrd"

	if len(prefix) > 0 {
		db = fmt.Sprintf("%s-%s", prefix, db)
	}

	afmeting := fmt.Sprintf("%d:%d", time.Now().Unix(), int(val))

	args := []string{
		fmt.Sprintf("--daemon=%s", addr),
		db, afmeting,
	}

	cmd := exec.Command("rrdupdate", args...)
	err := cmd.Run()
	if err == nil {
		return nil
	}

	log.Println("rrdupdate", args)

	stdout, _ := cmd.Output()

	exitErr := err.(*exec.ExitError)
	return fmt.Errorf(
		"rrdupdate err exit %d stdout %q stderr %q",
		exitErr.ExitCode(),
		string(stdout),
		string(exitErr.Stderr),
	)
}
