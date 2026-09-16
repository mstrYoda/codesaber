package acp

import (
	"bufio"
	"fmt"
	"golang.org/x/sys/windows"
	"os"
	"os/exec"
	"strconv"
	"strings"
	"testing"
	"time"
)

func TestWindowsProviderJobStopsDescendants(t *testing.T) {
	if mode := os.Getenv("CODESABER_JOB_TEST_CHILD"); mode != "" {
		if mode == "leaf" {
			time.Sleep(30 * time.Second)
			os.Exit(0)
		}
		_, _ = bufio.NewReader(os.Stdin).ReadString('\n')
		child := exec.Command(os.Args[0], "-test.run=^TestWindowsProviderJobStopsDescendants$")
		child.Env = replaceEnv(os.Environ(), "CODESABER_JOB_TEST_CHILD", "leaf")
		if err := child.Start(); err != nil {
			os.Exit(2)
		}
		fmt.Println(child.Process.Pid)
		os.Exit(0)
	}
	cmd := exec.Command(os.Args[0], "-test.run=^TestWindowsProviderJobStopsDescendants$")
	cmd.Env = replaceEnv(os.Environ(), "CODESABER_JOB_TEST_CHILD", "parent")
	configureChildProcess(cmd)
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		t.Fatal(err)
	}
	stdin, err := cmd.StdinPipe()
	if err != nil {
		t.Fatal(err)
	}
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	defer cmd.Process.Kill()
	cleanup, err := trackChildProcess(cmd)
	if err != nil {
		t.Fatal(err)
	}
	defer cleanup()
	fmt.Fprintln(stdin, "start")
	line, err := bufio.NewReader(stdout).ReadString('\n')
	if err != nil {
		t.Fatal(err)
	}
	pid, err := strconv.Atoi(strings.TrimSpace(line))
	if err != nil {
		t.Fatal(err)
	}
	process, err := windows.OpenProcess(windows.SYNCHRONIZE, false, uint32(pid))
	if err != nil {
		t.Fatal(err)
	}
	defer windows.CloseHandle(process)
	if err := cmd.Wait(); err != nil {
		t.Fatal(err)
	}
	cleanup()
	state, err := windows.WaitForSingleObject(process, 5000)
	if err != nil || state != windows.WAIT_OBJECT_0 {
		t.Fatalf("descendant survived job close: state=%d err=%v", state, err)
	}
}
