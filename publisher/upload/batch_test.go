package upload

import (
	"os"
	"path"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"

	"github.com/newrelic/infrastructure-publish-action/publisher/config"
	"github.com/newrelic/infrastructure-publish-action/publisher/lock"
	"github.com/newrelic/infrastructure-publish-action/publisher/release"
)

// countingLock records how many times the lock is taken.
type countingLock struct {
	locks, releases int
	lockErr         error
}

func (l *countingLock) Lock() error {
	l.locks++
	return l.lockErr
}

func (l *countingLock) Release() error {
	l.releases++
	return nil
}

func ohiJobs(t *testing.T, apps ...string) []AppUploadJob {
	t.Helper()
	schemas, err := config.ParseUploadSchemasFile("../../schemas/ohi.yml")
	require.NoError(t, err)

	var jobs []AppUploadJob
	for _, app := range apps {
		jobs = append(jobs, AppUploadJob{
			Conf: config.Config{
				AppName:             app,
				RepoName:            "newrelic/" + app,
				Tag:                 "v1.0.0",
				Version:             "1.0.0",
				DestPrefix:          "infrastructure_agent/batch_poc/",
				MirrorHost:          "http://mirror",
				AccessPointHost:     "http://mirror",
				ArtifactsSrcFolder:  "/assets",
				ArtifactsDestFolder: "/mnt/s3",
			},
			Schemas: schemas,
		})
	}
	return jobs
}

func Test_groupApt_oneGroupPerDistributionWithEveryApp(t *testing.T) {
	jobs := ohiJobs(t, "nri-apache", "nri-mysql")

	groups, err := groupApt(jobs)
	require.NoError(t, err)

	var osVersions []string
	for _, group := range groups {
		osVersions = append(osVersions, group.osVersion)
		require.Len(t, group.entries, 2, group.osVersion)
		assert.Equal(t, "nri-apache", group.entries[0].conf.AppName)
		assert.Equal(t, "nri-mysql", group.entries[1].conf.AppName)
		assert.Equal(t, []string{"amd64", "arm", "arm64"}, group.entries[0].archs)
	}
	// same distributions and order as the schema
	assert.Equal(t, jobs[0].Schemas[4].Uploads[0].OsVersion, osVersions)
}

func Test_groupApt_failsWhenAppsUseDifferentRepos(t *testing.T) {
	jobs := ohiJobs(t, "nri-apache", "nri-mysql")
	jobs[1].Conf.DestPrefix = "somewhere_else/"

	_, err := groupApt(jobs)
	assert.Error(t, err)
}

func Test_groupRpm_oneRepoPerPathWithEveryApp(t *testing.T) {
	jobs := ohiJobs(t, "nri-apache", "nri-mysql")

	repos := groupRpm(jobs)

	// (6 el + 13 sles + 2 amazonlinux) x 3 archs
	require.Len(t, repos, 21*3)
	byPath := map[string]rpmRepo{}
	for _, repo := range repos {
		require.Len(t, repo.files, 2)
		byPath[repo.files[0].destPath] = repo
	}

	repo, ok := byPath["infrastructure_agent/batch_poc/linux/yum/el/8/aarch64/"]
	require.True(t, ok, "arm64 packages go to the aarch64 folder")
	assert.Equal(t, rpmFile{
		srcPath:    "/assets/nri-apache-1.0.0-1.arm64.rpm",
		destPath:   "infrastructure_agent/batch_poc/linux/yum/el/8/aarch64/",
		s3RepoPath: "/mnt/s3/infrastructure_agent/batch_poc/linux/yum/el/8/aarch64",
		rpmPath:    "/mnt/s3/infrastructure_agent/batch_poc/linux/yum/el/8/aarch64/nri-apache-1.0.0-1.arm64.rpm",
	}, repo.files[0])
	assert.Equal(t, "/assets/nri-mysql-1.0.0-1.arm64.rpm", repo.files[1].srcPath)

	_, ok = byPath["infrastructure_agent/batch_poc/linux/zypp/sles/15.7/x86_64/"]
	assert.True(t, ok)
	_, ok = byPath["infrastructure_agent/batch_poc/linux/yum/amazonlinux/2023/arm/"]
	assert.True(t, ok)
}

func Test_newRpmFile_matchesSingleAppPaths(t *testing.T) {
	conf := ohiJobs(t, "nri-apache")[0].Conf
	upload := config.Upload{Type: config.TypeYum, Dest: "{dest_prefix}linux/yum/el/{os_version}/{arch}/"}

	rpm := newRpmFile(conf, "{app_name}-{version}-1.{arch}.rpm", upload, "x86_64", "9")

	assert.Equal(t, "/mnt/s3/infrastructure_agent/batch_poc/linux/yum/el/9/x86_64/nri-apache-1.0.0-1.x86_64.rpm", rpm.rpmPath)
	assert.Equal(t, generateRepoFileContent(conf.AccessPointHost, rpm.destPath), `[newrelic-infra]
name=New Relic Infrastructure
baseurl=http://mirror/infrastructure_agent/batch_poc/linux/yum/el/9/x86_64/
gpgkey=https://download.newrelic.com/infrastructure_agent/keys/newrelic_rpm_key_current.gpg
gpgcheck=1
repo_gpgcheck=1`)
}

func TestUploadArtifactsBatch_locksOnceAndMarksEveryApp(t *testing.T) {
	src := t.TempDir()
	dest := t.TempDir()
	schemas := config.UploadArtifactSchemas{
		{Src: "{app_name}-{arch}-{version}.txt", Arch: []string{"amd64"}, Uploads: []config.Upload{
			{Type: config.TypeFile, Dest: "{arch}/{app_name}/{src}"},
		}},
	}

	var jobs []AppUploadJob
	var marks []release.Mark
	marker := &MarkerMock{}
	for _, app := range []string{"nri-apache", "nri-mysql"} {
		conf := config.Config{AppName: app, RepoName: "newrelic/" + app, Tag: "v2.0.0", Version: "2.0.0", RunID: "42", Schema: "ohi",
			ArtifactsSrcFolder: src, ArtifactsDestFolder: dest}
		require.NoError(t, writeDummyFile(path.Join(src, app+"-amd64-2.0.0.txt")))
		jobs = append(jobs, AppUploadJob{Conf: conf, Schemas: schemas})

		mark := release.Mark{ReleaseInfo: release.ReleaseInfo{AppName: app}}
		marker.ShouldStart(release.ReleaseInfo{AppName: app, Tag: "v2.0.0", RunID: "42", RepoName: "newrelic/" + app, Schema: "ohi"}, mark)
		marks = append(marks, mark)
	}
	// one EndBatch for every app, never End one by one
	marker.ShouldEndBatch(marks)

	bucketLock := &countingLock{}
	err := UploadArtifactsBatch(jobs, bucketLock, marker)
	require.NoError(t, err)

	assert.Equal(t, 1, bucketLock.locks)
	assert.Equal(t, 1, bucketLock.releases)
	for _, app := range []string{"nri-apache", "nri-mysql"} {
		_, err = os.Stat(path.Join(dest, "amd64", app, app+"-amd64-2.0.0.txt"))
		assert.NoError(t, err)
	}
	mock.AssertExpectationsForObjects(t, marker)
}

func TestUploadArtifactsBatch_doesNothingWhenLockIsBusy(t *testing.T) {
	marker := &MarkerMock{}
	bucketLock := &countingLock{lockErr: lock.ErrLockBusy}

	err := UploadArtifactsBatch(ohiJobs(t, "nri-apache"), bucketLock, marker)

	assert.ErrorIs(t, err, lock.ErrLockBusy)
	assert.Equal(t, 0, bucketLock.releases)
	mock.AssertExpectationsForObjects(t, marker)
}

func TestUploadArtifactsBatch_emptyBatch(t *testing.T) {
	err := UploadArtifactsBatch(nil, &countingLock{}, &MarkerMock{})
	assert.ErrorIs(t, err, ErrEmptyBatch)
}
