package utils

import (
	"bufio"
	"context"
	"fmt"
	"io"
	"io/ioutil"
	"log"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

const (
	placeholderForOsVersion       = "{os_version}"
	placeholderForDestPrefix      = "{dest_prefix}"
	placeholderForRepoName        = "{repo_name}"
	placeholderForAppName         = "{app_name}"
	placeholderForArch            = "{arch}"
	placeholderForTag             = "{tag}"
	placeholderForVersion         = "{version}"
	PlaceholderForSrc             = "{src}"
	PlaceholderForAccessPointHost = "{access_point_host}"
)

var (
	Logger           = log.New(log.Writer(), "", 0)
)

func ReadFileContent(filePath string) ([]byte, error) {
	fileContent, err := ioutil.ReadFile(filePath)

	return fileContent, err
}

func ReplacePlaceholders(template, repoName, appName, arch, tag, version, destPrefix, osVersion string) (str string) {
	str = strings.Replace(template, placeholderForRepoName, repoName, -1)
	str = strings.Replace(str, placeholderForAppName, appName, -1)
	str = strings.Replace(str, placeholderForArch, arch, -1)
	str = strings.Replace(str, placeholderForTag, tag, -1)
	str = strings.Replace(str, placeholderForVersion, version, -1)
	str = strings.Replace(str, placeholderForDestPrefix, destPrefix, -1)
	str = strings.Replace(str, placeholderForOsVersion, osVersion, -1)

	return
}

// execLogOutput executes a command writing stdout & stderr to provided Logger.
func ExecLogOutput(l *log.Logger, cmdName string, commandTimeout time.Duration, cmdArgs ...string) (err error) {
	ctx, cancel := context.WithTimeout(context.Background(), commandTimeout)
	defer cancel()
	cmd := exec.CommandContext(ctx, cmdName, cmdArgs...)

	l.Printf("Executing in shell '%s'", cmd.String())

	stdoutR, err := cmd.StdoutPipe()
	if err != nil {
		return
	}
	stderrR, err := cmd.StderrPipe()
	if err != nil {
		return
	}

	wg := sync.WaitGroup{}
	wg.Add(2)
	defer l.Println()
	if err = cmd.Start(); err != nil {
		return err
	}

	go streamAsLog(&wg, l, stdoutR, "stdout")
	go streamAsLog(&wg, l, stderrR, "stderr")

	wg.Wait()
	return cmd.Wait()
}

func streamAsLog(wg *sync.WaitGroup, l *log.Logger, r io.ReadCloser, prefix string) {
	defer wg.Done()

	if prefix != "" {
		prefix += ": "
	}

	stdoutBufR := bufio.NewReader(r)
	var err error
	var line []byte
	for {
		line, _, err = stdoutBufR.ReadLine()
		if err != nil {
			if err == io.EOF {
				return
			}
			l.Fatalf("Got unknown error: %s", err)
		}

		l.Printf("%s%s\n", prefix, string(line))
	}
}

func CopyFile(srcPath string, destPath string, override bool) (err error) {

	// We do not want to override already pushed packages
	if _, err = os.Stat(destPath); !override && err == nil {
		Logger.Println(fmt.Sprintf("Skipping copying file '%s': already exists at:  %s", srcPath, destPath))
		return
	}

	destDirectory := filepath.Dir(destPath)

	if _, err = os.Stat(destDirectory); os.IsNotExist(err) {
		// set right permissions
		err = os.MkdirAll(destDirectory, 0744)
		if err != nil {
			return err
		}
	}

	Logger.Println("[ ] Copy " + srcPath + " into " + destPath)
	input, err := ioutil.ReadFile(srcPath)
	if err != nil {
		return err
	}

	err = ioutil.WriteFile(destPath, input, 0744)
	if err != nil {
		return err
	}

	Logger.Println("[✔] Copy " + srcPath + " into " + destPath)
	return nil
}
