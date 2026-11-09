package upload

import (
	"bytes"
	"compress/gzip"
	"errors"
	"fmt"
	"io"
	"log"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/newrelic/infrastructure-publish-action/publisher/utils"
)

// Guards for the S3 mount in batch mode. A batch runs for hours: the mount can drop in the middle of a
// repository ("Transport endpoint is not connected", e.g. when its temporary credentials expire). createrepo
// then skips the packages it cannot read and still exits successfully, so a truncated index would be signed
// and published. The guards below make that a retry (after a remount) or a failed run.

const (
	// A batch mount is refreshed before it gets this old, the credentials of the mount can be as short as 1 hour.
	mountMaxAge = 40 * time.Minute
	// How many times a repository index is built again, remounting S3 in between, before the run fails.
	rpmIndexTries = 3
)

// errBadIndex means the metadata createrepo built does not list the packages of the repository.
var errBadIndex = errors.New("incomplete repository index")

var (
	remountS3 = utils.S3RemountFn
	nowFn     = time.Now
	mountedAt = time.Now()
)

// ensureFreshMount remounts the bucket when the mount is no longer connected or is older than mountMaxAge.
func ensureFreshMount(mountDir string) error {
	healthy := mountHealthy(mountDir)
	if healthy && nowFn().Sub(mountedAt) < mountMaxAge {
		return nil
	}
	if healthy {
		utils.Logger.Printf("[batch] the S3 mount is %s old, remounting it", nowFn().Sub(mountedAt).Round(time.Second))
	} else {
		utils.Logger.Printf("[batch] the S3 mount %s is not connected, remounting it", mountDir)
	}
	return forceRemount(mountDir)
}

// forceRemount remounts the bucket whatever the age of the mount is.
func forceRemount(mountDir string) error {
	remountS3(utils.Logger, commandTimeout)
	mountedAt = nowFn()
	if !mountHealthy(mountDir) {
		return fmt.Errorf("the S3 mount %s is not available after remounting it", mountDir)
	}
	return nil
}

// mountHealthy reads the mount point, which fails on a dropped FUSE mount.
func mountHealthy(mountDir string) bool {
	dir, err := os.Open(mountDir)
	if err != nil {
		return false
	}
	defer dir.Close()
	if _, err = dir.Readdirnames(1); err != nil && err != io.EOF {
		return false
	}
	return true
}

// countRpmFiles is the number of rpm packages in the repository folder.
func countRpmFiles(repoDir string) (int, error) {
	entries, err := os.ReadDir(repoDir)
	if err != nil {
		return 0, err
	}
	count := 0
	for _, entry := range entries {
		if !entry.IsDir() && strings.HasSuffix(entry.Name(), ".rpm") {
			count++
		}
	}
	return count, nil
}

var packagesAttr = regexp.MustCompile(`packages="(\d+)"`)

// indexedPackages is the number of packages listed in the primary metadata of a repodata folder.
func indexedPackages(repodataDir string) (int, error) {
	matches, err := filepath.Glob(filepath.Join(repodataDir, "*primary.xml.gz"))
	if err != nil {
		return 0, err
	}
	if len(matches) == 0 {
		return 0, fmt.Errorf("no primary.xml.gz in %s", repodataDir)
	}
	file, err := os.Open(matches[0])
	if err != nil {
		return 0, err
	}
	defer file.Close()
	reader, err := gzip.NewReader(file)
	if err != nil {
		return 0, err
	}
	defer reader.Close()
	// the package count is an attribute of the root element, at the start of the file
	head := make([]byte, 4096)
	n, err := io.ReadFull(reader, head)
	if err != nil && err != io.ErrUnexpectedEOF && err != io.EOF {
		return 0, err
	}
	found := packagesAttr.FindSubmatch(head[:n])
	if found == nil {
		return 0, fmt.Errorf("no package count in %s", matches[0])
	}
	return strconv.Atoi(string(found[1]))
}

// createrepoReadErrors reports the packages createrepo could not read: it logs them and exits successfully.
func createrepoReadErrors(output string) error {
	for _, line := range strings.Split(output, "\n") {
		if strings.Contains(line, "Critical:") || strings.Contains(line, "Cannot read package") ||
			strings.Contains(line, "Transport endpoint is not connected") {
			return fmt.Errorf("%w: createrepo could not read the repository: %s", errBadIndex, strings.TrimSpace(line))
		}
	}
	return nil
}

// checkRpmIndex compares the metadata createrepo built with the repository: every rpm file has to be listed
// and no package of the previous index can be gone.
func checkRpmIndex(repoDir, newRepodataDir string, previouslyIndexed int, createrepoOutput string) error {
	if err := createrepoReadErrors(createrepoOutput); err != nil {
		return err
	}
	files, err := countRpmFiles(repoDir)
	if err != nil {
		return fmt.Errorf("%w: cannot list %s: %v", errBadIndex, repoDir, err)
	}
	indexed, err := indexedPackages(newRepodataDir)
	if err != nil {
		return fmt.Errorf("%w: cannot read the new metadata: %v", errBadIndex, err)
	}
	if indexed != files {
		return fmt.Errorf("%w: %s has %d rpm files but the new index lists %d", errBadIndex, repoDir, files, indexed)
	}
	if indexed < previouslyIndexed {
		return fmt.Errorf("%w: the new index of %s lists %d packages, the previous one %d", errBadIndex, repoDir, indexed, previouslyIndexed)
	}
	return nil
}

// runCreaterepo runs createrepo logging its output as usual and returns the output too.
func runCreaterepo(args ...string) (string, error) {
	var output bytes.Buffer
	capture := log.New(io.MultiWriter(utils.Logger.Writer(), &output), utils.Logger.Prefix(), utils.Logger.Flags())
	err := utils.ExecLogOutput(capture, "createrepo", commandTimeout, args...)
	return output.String(), err
}

// retryBadIndex runs attempt again, after cleanup and a remount, while it fails with errBadIndex.
// Any other error is returned right away.
func retryBadIndex(tries int, attempt func() error, cleanup func(), remount func() error) (err error) {
	for i := 1; i <= tries; i++ {
		if err = attempt(); err == nil || !errors.Is(err, errBadIndex) {
			return err
		}
		utils.Logger.Printf("[batch] attempt %d of %d: %v", i, tries, err)
		cleanup()
		if i < tries {
			if remountErr := remount(); remountErr != nil {
				return fmt.Errorf("%v; %w", remountErr, err)
			}
		}
	}
	return fmt.Errorf("repository index still incomplete after %d attempts: %w", tries, err)
}
