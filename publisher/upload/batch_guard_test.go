package upload

import (
	"bytes"
	"compress/gzip"
	"errors"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/newrelic/infrastructure-publish-action/publisher/utils"
)

func writeRpms(t *testing.T, dir string, count int) {
	t.Helper()
	for i := 0; i < count; i++ {
		require.NoError(t, os.WriteFile(filepath.Join(dir, fmt.Sprintf("nri-test-1.0.%d-1.x86_64.rpm", i)), []byte("rpm"), 0644))
	}
	// not packages
	require.NoError(t, os.WriteFile(filepath.Join(dir, "newrelic-infra.repo"), []byte("[repo]"), 0644))
	require.NoError(t, os.Mkdir(filepath.Join(dir, "repodata"), 0755))
}

func writePrimary(t *testing.T, repodataDir string, packages int) {
	t.Helper()
	require.NoError(t, os.MkdirAll(repodataDir, 0755))
	file, err := os.Create(filepath.Join(repodataDir, "abc123-primary.xml.gz"))
	require.NoError(t, err)
	defer file.Close()
	writer := gzip.NewWriter(file)
	_, err = fmt.Fprintf(writer, `<?xml version="1.0" encoding="UTF-8"?>
<metadata xmlns="http://linux.duke.edu/metadata/common" xmlns:rpm="http://linux.duke.edu/metadata/rpm" packages="%d">
<package type="rpm"><name>nri-test</name></package>
</metadata>`, packages)
	require.NoError(t, err)
	require.NoError(t, writer.Close())
}

func Test_countRpmFiles_countsOnlyRpmPackages(t *testing.T) {
	dir := t.TempDir()
	writeRpms(t, dir, 4)

	count, err := countRpmFiles(dir)

	require.NoError(t, err)
	assert.Equal(t, 4, count)
}

func Test_countRpmFiles_failsOnMissingFolder(t *testing.T) {
	_, err := countRpmFiles(filepath.Join(t.TempDir(), "missing"))
	assert.Error(t, err)
}

func Test_indexedPackages_readsTheCountOfThePrimaryMetadata(t *testing.T) {
	dir := t.TempDir()
	writePrimary(t, dir, 1911)

	count, err := indexedPackages(dir)

	require.NoError(t, err)
	assert.Equal(t, 1911, count)
}

func Test_indexedPackages_failsWithoutMetadata(t *testing.T) {
	_, err := indexedPackages(t.TempDir())
	assert.Error(t, err)
}

func Test_createrepoReadErrors(t *testing.T) {
	assert.NoError(t, createrepoReadErrors("stdout: Directory walk started\nstdout: Pool started\n"))

	for _, line := range []string{
		"stderr: C_CREATEREPOLIB: Critical: Stat() on /mnt/s3/x/nri-a-1.rpm: Transport endpoint is not connected",
		"stderr: C_CREATEREPOLIB: Warning: Cannot read package: /mnt/s3/x/nri-a-1.rpm: Fopen failed",
		"stderr: error: read failed: Transport endpoint is not connected",
	} {
		err := createrepoReadErrors("stdout: ok\n" + line + "\n")
		assert.ErrorIs(t, err, errBadIndex, line)
	}
}

func Test_checkRpmIndex(t *testing.T) {
	tests := map[string]struct {
		files, indexed, previous int
		output                   string
		wantErr                  bool
	}{
		"every package is listed":               {files: 5, indexed: 5, previous: 4},
		"first index of a repository":           {files: 5, indexed: 5, previous: 0},
		"index lists only part of the packages": {files: 1917, indexed: 445, previous: 1911, wantErr: true},
		"index lists more than the files":       {files: 3, indexed: 5, previous: 3, wantErr: true},
		"createrepo could not read a package":   {files: 5, indexed: 5, previous: 4, output: "stderr: C_CREATEREPOLIB: Critical: Stat() on /x: Transport endpoint is not connected", wantErr: true},
	}
	for name, tt := range tests {
		t.Run(name, func(t *testing.T) {
			repo := t.TempDir()
			writeRpms(t, repo, tt.files)
			repodata := t.TempDir()
			writePrimary(t, repodata, tt.indexed)

			err := checkRpmIndex(repo, repodata, tt.previous, tt.output)

			if tt.wantErr {
				assert.ErrorIs(t, err, errBadIndex)
				return
			}
			assert.NoError(t, err)
		})
	}
}

func Test_checkRpmIndex_failsWhenTheIndexShrinks(t *testing.T) {
	repo := t.TempDir()
	writeRpms(t, repo, 3)
	repodata := t.TempDir()
	writePrimary(t, repodata, 3)

	// the folder listing and the index agree, but the previous index had more packages
	err := checkRpmIndex(repo, repodata, 10, "")

	assert.ErrorIs(t, err, errBadIndex)
}

func Test_checkRpmIndex_failsWhenTheFolderCannotBeListed(t *testing.T) {
	repodata := t.TempDir()
	writePrimary(t, repodata, 3)

	err := checkRpmIndex(filepath.Join(t.TempDir(), "gone"), repodata, 0, "")

	assert.ErrorIs(t, err, errBadIndex)
}

func Test_retryBadIndex(t *testing.T) {
	bad := fmt.Errorf("%w: partial", errBadIndex)

	t.Run("builds again, remounting in between, until the index is complete", func(t *testing.T) {
		attempts, cleanups, remounts := 0, 0, 0
		err := retryBadIndex(3,
			func() error {
				attempts++
				if attempts < 3 {
					return bad
				}
				return nil
			},
			func() { cleanups++ },
			func() error { remounts++; return nil })

		require.NoError(t, err)
		assert.Equal(t, 3, attempts)
		assert.Equal(t, 2, cleanups)
		assert.Equal(t, 2, remounts)
	})

	t.Run("fails after the last attempt", func(t *testing.T) {
		attempts, remounts := 0, 0
		err := retryBadIndex(3,
			func() error { attempts++; return bad },
			func() {},
			func() error { remounts++; return nil })

		assert.ErrorIs(t, err, errBadIndex)
		assert.Equal(t, 3, attempts)
		assert.Equal(t, 2, remounts, "no remount after the last attempt")
	})

	t.Run("does not retry other errors", func(t *testing.T) {
		attempts := 0
		other := errors.New("gpg failed")
		err := retryBadIndex(3,
			func() error { attempts++; return other },
			func() {},
			func() error { return nil })

		assert.ErrorIs(t, err, other)
		assert.Equal(t, 1, attempts)
	})

	t.Run("fails when the remount fails", func(t *testing.T) {
		attempts := 0
		err := retryBadIndex(3,
			func() error { attempts++; return bad },
			func() {},
			func() error { return errors.New("mount is gone") })

		assert.Error(t, err)
		assert.ErrorIs(t, err, errBadIndex)
		assert.Equal(t, 1, attempts)
	})
}

func Test_ensureFreshMount(t *testing.T) {
	origRemount, origNow, origMountedAt := remountS3, nowFn, mountedAt
	t.Cleanup(func() { remountS3, nowFn, mountedAt = origRemount, origNow, origMountedAt })

	base := time.Date(2026, 11, 9, 10, 0, 0, 0, time.UTC)
	now := base
	nowFn = func() time.Time { return now }
	remounts := 0
	remountS3 = func(*log.Logger, time.Duration) { remounts++ }
	mountDir := t.TempDir()

	t.Run("a young healthy mount is kept", func(t *testing.T) {
		mountedAt, remounts = base, 0
		now = base.Add(mountMaxAge - time.Minute)

		require.NoError(t, ensureFreshMount(mountDir))

		assert.Equal(t, 0, remounts)
	})

	t.Run("an old mount is remounted and starts counting again", func(t *testing.T) {
		mountedAt, remounts = base, 0
		now = base.Add(mountMaxAge + time.Minute)

		require.NoError(t, ensureFreshMount(mountDir))

		assert.Equal(t, 1, remounts)
		assert.Equal(t, now, mountedAt)
	})

	t.Run("a dropped mount is remounted whatever its age", func(t *testing.T) {
		mountedAt, remounts = base, 0
		now = base.Add(time.Minute)

		// the mount point does not exist, so it is neither healthy before nor after the remount
		err := ensureFreshMount(filepath.Join(t.TempDir(), "not-mounted"))

		assert.Equal(t, 1, remounts)
		assert.Error(t, err, "still not available after remounting")
	})
}

func Test_createrepoUpdateArgs(t *testing.T) {
	assert.Equal(t,
		[]string{"--update", "-s", "sha", "/mnt/s3/repo", "-o", "/tmp"},
		createrepoUpdateArgs("/mnt/s3/repo", "/tmp"))
}

// writePrimaryEntries writes a primary.xml.gz like createrepo does: the package count in the root element
// and one entry per package with its location and size.
func writePrimaryEntries(t *testing.T, repodataDir string, header int, entries map[string]int64) {
	t.Helper()
	require.NoError(t, os.MkdirAll(repodataDir, 0755))
	file, err := os.Create(filepath.Join(repodataDir, "abc123-primary.xml.gz"))
	require.NoError(t, err)
	defer file.Close()
	writer := gzip.NewWriter(file)
	_, err = fmt.Fprintf(writer, `<?xml version="1.0" encoding="UTF-8"?>
<metadata xmlns="http://linux.duke.edu/metadata/common" xmlns:rpm="http://linux.duke.edu/metadata/rpm" packages="%d">`, header)
	require.NoError(t, err)
	for name, size := range entries {
		_, err = fmt.Fprintf(writer, `<package type="rpm"><name>x</name><size package="%d" installed="1" archive="1"/><location href="%s"/></package>`, size, name)
		require.NoError(t, err)
	}
	_, err = fmt.Fprint(writer, `</metadata>`)
	require.NoError(t, err)
	require.NoError(t, writer.Close())
}

func writeRpmOfSize(t *testing.T, dir, name string, size int) {
	t.Helper()
	require.NoError(t, os.WriteFile(filepath.Join(dir, name), make([]byte, size), 0644))
}

func Test_indexedFiles_readsLocationAndSize(t *testing.T) {
	dir := t.TempDir()
	writePrimaryEntries(t, dir, 2, map[string]int64{"nri-a-1.0.0-1.x86_64.rpm": 100, "nri-b-2.0.0-1.x86_64.rpm": 250})

	files, err := indexedFiles(dir)

	require.NoError(t, err)
	assert.Equal(t, map[string]int64{"nri-a-1.0.0-1.x86_64.rpm": 100, "nri-b-2.0.0-1.x86_64.rpm": 250}, files)
}

func Test_diagnoseIndex_logsWhatIsMissing(t *testing.T) {
	var output bytes.Buffer
	orig := utils.Logger
	utils.Logger = log.New(&output, "", 0)
	t.Cleanup(func() { utils.Logger = orig })

	repo := t.TempDir()
	for _, name := range []string{"nri-a-1.rpm", "nri-b-1.rpm", "nri-c-1.rpm", "nri-d-1.rpm"} {
		writeRpmOfSize(t, repo, name, 10)
	}
	newIndex := filepath.Join(t.TempDir(), "repodata")
	// the index stops after nri-b
	writePrimaryEntries(t, newIndex, 2, map[string]int64{"nri-a-1.rpm": 10, "nri-b-1.rpm": 10})

	diagnoseIndex(repo, newIndex)

	logged := output.String()
	assert.Contains(t, logged, "rpm files: 4, packages in the index header: 2, entries in the index: 2, files missing from the index: 2")
	assert.Contains(t, logged, "the index goes from nri-a-1.rpm to nri-b-1.rpm")
	assert.Contains(t, logged, "first missing files: nri-c-1.rpm, nri-d-1.rpm")
	assert.Contains(t, logged, "abc123-primary.xml.gz")
}

func Test_diagnoseIndex_doesNotFailWithoutMetadata(t *testing.T) {
	var output bytes.Buffer
	orig := utils.Logger
	utils.Logger = log.New(&output, "", 0)
	t.Cleanup(func() { utils.Logger = orig })

	assert.NotPanics(t, func() { diagnoseIndex(t.TempDir(), filepath.Join(t.TempDir(), "missing")) })
	assert.Contains(t, output.String(), "incomplete index")
}
