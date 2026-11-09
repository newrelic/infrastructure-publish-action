package upload

import (
	"bytes"
	"compress/gzip"
	"encoding/xml"
	"errors"
	"fmt"
	"io"
	"log"
	"os"
	"path/filepath"
	"regexp"
	"sort"
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
	// The attempt that trusts the old index entries instead of reading every package again (createrepo
	// --skip-stat). The attempts before it read every package, as the single-app publish does.
	skipStatFromAttempt = 3
	// How many names of missing packages are logged when an index is incomplete.
	diagnoseNames = 10
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

// createrepoUpdateArgs are the arguments of the createrepo run that rebuilds the index of a repository.
// A sync or a copy of the repository gives every package a new modified time, and createrepo --update reads
// again each package whose time differs from the one in its index entry: gigabytes over the S3 mount for every
// repository. skipStat makes it keep the entry of a package that is listed under the same file name and only
// read the new ones. Published packages never change, so that is safe unless one of them is overwritten.
func createrepoUpdateArgs(repoDir, outputDir string, skipStat bool) []string {
	args := []string{"--update"}
	if skipStat {
		args = append(args, "--skip-stat")
	}
	return append(args, "-s", "sha", repoDir, "-o", outputDir)
}

// canSkipStat is false when the batch overwrites a package that may already be in the index.
func canSkipStat(files []rpmFile) bool {
	for _, file := range files {
		if file.override {
			return false
		}
	}
	return true
}

// skipStatForAttempt tells if an attempt trusts the old index entries: only from skipStatFromAttempt on,
// and only when the batch does not overwrite any package.
func skipStatForAttempt(allowed bool, attempt int) bool {
	return allowed && attempt >= skipStatFromAttempt
}

type primaryMetadata struct {
	Packages []struct {
		Location struct {
			Href string `xml:"href,attr"`
		} `xml:"location"`
		Size struct {
			Package int64 `xml:"package,attr"`
		} `xml:"size"`
	} `xml:"package"`
}

// indexedFiles is the size recorded in the index of every package, by file name.
func indexedFiles(repodataDir string) (map[string]int64, error) {
	matches, err := filepath.Glob(filepath.Join(repodataDir, "*primary.xml.gz"))
	if err != nil {
		return nil, err
	}
	if len(matches) == 0 {
		return nil, fmt.Errorf("no primary.xml.gz in %s", repodataDir)
	}
	file, err := os.Open(matches[0])
	if err != nil {
		return nil, err
	}
	defer file.Close()
	reader, err := gzip.NewReader(file)
	if err != nil {
		return nil, err
	}
	defer reader.Close()
	var metadata primaryMetadata
	if err = xml.NewDecoder(reader).Decode(&metadata); err != nil {
		return nil, err
	}
	files := make(map[string]int64, len(metadata.Packages))
	for _, pkg := range metadata.Packages {
		files[filepath.Base(pkg.Location.Href)] = pkg.Size.Package
	}
	return files, nil
}

// sizeMismatches lists the rpm files of a repository whose size is not the one in their index entry.
// Files that are not in the index yet are not compared.
func sizeMismatches(repoDir string, indexed map[string]int64) ([]string, error) {
	entries, err := os.ReadDir(repoDir)
	if err != nil {
		return nil, err
	}
	var mismatches []string
	for _, entry := range entries {
		want, ok := indexed[entry.Name()]
		if entry.IsDir() || !ok {
			continue
		}
		info, infoErr := entry.Info()
		if infoErr != nil {
			return nil, infoErr
		}
		if info.Size() != want {
			mismatches = append(mismatches, fmt.Sprintf("%s: file has %d bytes, index entry %d", entry.Name(), info.Size(), want))
		}
	}
	sort.Strings(mismatches)
	return mismatches, nil
}

// trustOldIndex is true when createrepo can keep the old index entries without reading the packages: the old
// index is there and every package in it has the size of its file. A package that was cut short or replaced
// changes the size, so it still makes createrepo read everything.
func trustOldIndex(repoDir, oldRepodataDir string) (bool, string) {
	indexed, err := indexedFiles(oldRepodataDir)
	if err != nil {
		return false, fmt.Sprintf("cannot read the old index: %v", err)
	}
	mismatches, err := sizeMismatches(repoDir, indexed)
	if err != nil {
		return false, fmt.Sprintf("cannot compare the file sizes: %v", err)
	}
	if len(mismatches) > 0 {
		return false, fmt.Sprintf("%d files do not match their index entry, first: %s", len(mismatches), mismatches[0])
	}
	return true, fmt.Sprintf("the %d packages of the old index have the size of their file", len(indexed))
}

// diagnoseIndex logs what is in the repository folder and in the new metadata when the index is incomplete,
// to find which packages are missing and where the index stops.
func diagnoseIndex(repoDir, newRepodataDir string) {
	l := utils.Logger
	l.Printf("[batch][debug] ---- incomplete index of %s ----", repoDir)

	var files []string
	if entries, err := os.ReadDir(repoDir); err != nil {
		l.Printf("[batch][debug] cannot list %s: %v", repoDir, err)
	} else {
		for _, entry := range entries {
			if !entry.IsDir() && strings.HasSuffix(entry.Name(), ".rpm") {
				files = append(files, entry.Name())
			}
		}
	}
	sort.Strings(files)

	for _, dir := range []string{newRepodataDir, filepath.Join(filepath.Dir(newRepodataDir), ".repodata")} {
		entries, err := os.ReadDir(dir)
		if err != nil {
			l.Printf("[batch][debug] %s: %v", dir, err)
			continue
		}
		var listing []string
		for _, entry := range entries {
			size := int64(-1)
			if info, infoErr := entry.Info(); infoErr == nil {
				size = info.Size()
			}
			listing = append(listing, fmt.Sprintf("%s (%d bytes)", entry.Name(), size))
		}
		l.Printf("[batch][debug] %s has %d files: %s", dir, len(listing), strings.Join(listing, ", "))
	}

	if primaries, _ := filepath.Glob(filepath.Join(newRepodataDir, "*primary.xml.gz")); len(primaries) != 1 {
		l.Printf("[batch][debug] %d primary.xml.gz files in the new metadata: %v", len(primaries), primaries)
	}
	header, headerErr := indexedPackages(newRepodataDir)
	indexed, indexErr := indexedFiles(newRepodataDir)
	if headerErr != nil || indexErr != nil {
		l.Printf("[batch][debug] cannot read the new index: header: %v, entries: %v", headerErr, indexErr)
		return
	}
	var names, missing []string
	for name := range indexed {
		names = append(names, name)
	}
	sort.Strings(names)
	for _, file := range files {
		if _, ok := indexed[file]; !ok {
			missing = append(missing, file)
		}
	}
	l.Printf("[batch][debug] rpm files: %d, packages in the index header: %d, entries in the index: %d, files missing from the index: %d",
		len(files), header, len(indexed), len(missing))
	if len(names) > 0 {
		l.Printf("[batch][debug] the index goes from %s to %s", names[0], names[len(names)-1])
	}
	if len(missing) > 0 {
		shown := missing
		if len(shown) > diagnoseNames {
			shown = shown[:diagnoseNames]
		}
		l.Printf("[batch][debug] first missing files: %s", strings.Join(shown, ", "))
		l.Printf("[batch][debug] last missing file: %s", missing[len(missing)-1])
	}
	if mismatches, err := sizeMismatches(repoDir, indexed); err == nil && len(mismatches) > 0 {
		l.Printf("[batch][debug] %d indexed files have another size than their index entry, first: %s", len(mismatches), mismatches[0])
	}
	logSystemState()
}

// logSystemState logs the disk and memory of the machine; an index cut short can come from a full /tmp.
func logSystemState() {
	for _, command := range [][]string{{"df", "-h", "/tmp", "/mnt/s3"}, {"free", "-m"}} {
		_ = utils.ExecLogOutput(utils.Logger, command[0], time.Minute, command[1:]...)
	}
}
