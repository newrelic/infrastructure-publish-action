package utils

import (
	"bufio"
	"context"
	"fmt"
	"io"
	"io/ioutil"
	"log"
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

	s3RetrySleepTimeout = 3 * time.Second
	s3Retries           = 10
)

var (
	Logger = log.New(log.Writer(), "", 0)
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

func CopyFile(srcPath string, destPath string, override bool, commandTimeout time.Duration) (err error) {

	destDirectory := filepath.Dir(destPath)

	Logger.Println("[ ] Create " + destDirectory)

	if err = ExecWithRetries(s3Retries, S3RemountFn, Logger, "mkdir", commandTimeout, "-p", destDirectory); err != nil {
		return err
	}

	Logger.Println("[✔] Create " + destDirectory)

	Logger.Println("[ ] Copy " + srcPath + " into " + destPath)

	if override {
		if err = ExecWithRetries(s3Retries, S3RemountFn, Logger, "cp", commandTimeout, "-f", srcPath, destPath); err != nil {
			return err
		}
	} else {
		// Note: we are not doing retries here as this command is not
		// idempotent. If one copy fails, retry will skip and leave corrupted
		// file in the repo
		Logger.Println(fmt.Sprintf("Try to copy file '%s' to %s, but skipping if exists", srcPath, destPath))
		if err = ExecLogOutput(Logger, "cp", commandTimeout, "-n", srcPath, destPath); err != nil {
			return err
		}
	}

	Logger.Println("[✔] Copy " + srcPath + " into " + destPath)
	return nil
}

func ExecWithRetries(retries int, s3Remount RetryCallback, l *log.Logger, cmdName string, commandTimeout time.Duration, cmdArgs ...string) error {
	var err error
	for i := 0; i < retries; i++ {
		err = ExecLogOutput(l, cmdName, commandTimeout, cmdArgs...)
		if err == nil {
			break
		}
		time.Sleep(s3RetrySleepTimeout)
		s3Remount(l, commandTimeout)
		l.Printf("[attempt %v] error executing command %s %s", i, cmdName, strings.Join(cmdArgs, " "))
	}
	return err
}

type RetryCallback func(l *log.Logger, commandTimeout time.Duration)

func S3RemountFn(l *log.Logger, commandTimeout time.Duration) {
	err := ExecLogOutput(l, "make", commandTimeout, "unmount-s3")
	if err != nil {
		l.Printf("unmounting s3 failed %v", err)
	}

	err = ExecLogOutput(l, "make", commandTimeout, "mount-s3", "mount-s3-check")
	if err != nil {
		l.Printf("mounting s3 failed %v", err)
	}
}
