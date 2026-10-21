package upload

import (
	"errors"
	"fmt"
	"time"

	"github.com/newrelic/infrastructure-publish-action/publisher/config"
	"github.com/newrelic/infrastructure-publish-action/publisher/lock"
	"github.com/newrelic/infrastructure-publish-action/publisher/release"
	"github.com/newrelic/infrastructure-publish-action/publisher/utils"
)

var ErrEmptyBatch = errors.New("no apps to publish")

// AppUploadJob is one app of a batch: its own config (app name, tag, version...) and schema.
type AppUploadJob struct {
	Conf    config.Config
	Schemas config.UploadArtifactSchemas
}

// aptEntry is one app's package (all its archs) going into one apt distribution.
type aptEntry struct {
	conf        config.Config
	srcTemplate string
	upload      config.Upload
	archs       []string
}

type aptGroup struct {
	osVersion string
	entries   []aptEntry
}

// rpmRepo is one yum/zypp repository with every package the batch adds into it.
type rpmRepo struct {
	conf  config.Config
	files []rpmFile
}

// UploadArtifactsBatch publishes several apps with the same steps as UploadArtifacts, but takes
// the lock once and builds every repository once for all the apps:
//   - apt: per distribution, create + mirror once, add every app's packages, publish + sync once
//   - yum/zypp: per repository, copy every app's packages, createrepo + sign once
//   - file: copied per app, as today
func UploadArtifactsBatch(jobs []AppUploadJob, bucketLock lock.BucketLock, releaseMarker release.Marker) (err error) {
	if len(jobs) == 0 {
		return ErrEmptyBatch
	}

	aptGroups, err := groupApt(jobs)
	if err != nil {
		return err
	}
	rpmRepos := groupRpm(jobs)

	if err = bucketLock.Lock(); err != nil {
		return
	}

	var marks []release.Mark
	defer func() {
		for _, mark := range marks {
			if markerErr := releaseMarker.End(mark); markerErr != nil {
				utils.Logger.Printf("ERROR: cannot end release marker %v", markerErr)
			}
		}

		errRelease := bucketLock.Release()
		if err == nil {
			err = errRelease
		} else if errRelease != nil {
			err = fmt.Errorf("got 2 errors: uploading: \"%v\", releasing lock: \"%v\"", err, errRelease)
		}
	}()

	// Write one release marker per app, as a single-app run does
	for _, job := range jobs {
		mark, markErr := releaseMarker.Start(
			release.ReleaseInfo{
				AppName:   job.Conf.AppName,
				Tag:       job.Conf.Tag,
				RunID:     job.Conf.RunID,
				RepoName:  job.Conf.RepoName,
				Schema:    job.Conf.Schema,
				SchemaURL: job.Conf.SchemaURL,
			},
		)
		if markErr != nil {
			return fmt.Errorf("cannot start release marker for %s: %w", job.Conf.AppName, markErr)
		}
		marks = append(marks, mark)
	}

	start := time.Now()
	for _, job := range jobs {
		for _, artifactSchema := range job.Schemas {
			for _, upload := range artifactSchema.Uploads {
				if upload.Type != config.TypeFile {
					continue
				}
				if err = uploadArtifact(job.Conf, artifactSchema, upload); err != nil {
					return err
				}
			}
		}
	}
	utils.Logger.Printf("[batch] file uploads for %d apps took %s", len(jobs), time.Since(start).Round(time.Second))

	for _, group := range aptGroups {
		start = time.Now()
		if err = uploadAptBatch(group); err != nil {
			return err
		}
		utils.Logger.Printf("[batch] apt %s with %d packages took %s", group.osVersion, len(group.entries), time.Since(start).Round(time.Second))
	}

	for _, repo := range rpmRepos {
		start = time.Now()
		if err = uploadRpmBatch(repo); err != nil {
			return err
		}
		utils.Logger.Printf("[batch] rpm %s with %d packages took %s", repo.files[0].destPath, len(repo.files), time.Since(start).Round(time.Second))
	}

	return nil
}

// groupApt collects every app's apt uploads per distribution, keeping the schema order.
func groupApt(jobs []AppUploadJob) ([]aptGroup, error) {
	var groups []aptGroup
	index := map[string]int{}
	for _, job := range jobs {
		for _, artifactSchema := range job.Schemas {
			for _, upload := range artifactSchema.Uploads {
				if upload.Type != config.TypeApt {
					continue
				}
				for _, osVersion := range upload.OsVersion {
					i, ok := index[osVersion]
					if !ok {
						i = len(groups)
						index[osVersion] = i
						groups = append(groups, aptGroup{osVersion: osVersion})
					}
					groups[i].entries = append(groups[i].entries, aptEntry{
						conf:        job.Conf,
						srcTemplate: artifactSchema.Src,
						upload:      upload,
						archs:       artifactSchema.Arch,
					})
				}
			}
		}
	}

	// One distribution is mirrored and published once, so every app has to agree on where from and where to.
	for _, group := range groups {
		first := group.entries[0]
		firstSrc := generateAptSrcRepoUrl(first.upload.SrcRepo, first.conf.MirrorHost)
		firstDest := utils.ReplacePlaceholders(first.upload.Dest, "", "", "", "", "", first.conf.DestPrefix, group.osVersion)
		for _, entry := range group.entries[1:] {
			src := generateAptSrcRepoUrl(entry.upload.SrcRepo, entry.conf.MirrorHost)
			dest := utils.ReplacePlaceholders(entry.upload.Dest, "", "", "", "", "", entry.conf.DestPrefix, group.osVersion)
			if src != firstSrc || dest != firstDest {
				return nil, fmt.Errorf("apt %s: %s (src %s, dest %s) and %s (src %s, dest %s) must use the same apt repo to be batched",
					group.osVersion, first.conf.AppName, firstSrc, firstDest, entry.conf.AppName, src, dest)
			}
		}
	}
	return groups, nil
}

// groupRpm collects every app's rpm packages per repository path, keeping the schema order.
func groupRpm(jobs []AppUploadJob) []rpmRepo {
	var repos []rpmRepo
	index := map[string]int{}
	for _, job := range jobs {
		for _, artifactSchema := range job.Schemas {
			for _, upload := range artifactSchema.Uploads {
				if upload.Type != config.TypeYum && upload.Type != config.TypeZypp {
					continue
				}
				for _, arch := range artifactSchema.Arch {
					for _, osVersion := range upload.OsVersion {
						rpm := newRpmFile(job.Conf, artifactSchema.Src, upload, arch, osVersion)
						i, ok := index[rpm.s3RepoPath]
						if !ok {
							i = len(repos)
							index[rpm.s3RepoPath] = i
							repos = append(repos, rpmRepo{conf: job.Conf})
						}
						repos[i].files = append(repos[i].files, rpm)
					}
				}
			}
		}
	}
	return repos
}

func uploadAptBatch(group aptGroup) (err error) {
	first := group.entries[0]
	utils.Logger.Printf("[ ] Start uploading %d debs for os %s", len(group.entries), group.osVersion)

	if err = createAptRepo(first.conf, first.upload, group.osVersion); err != nil {
		return err
	}

	var destPath string
	for _, entry := range group.entries {
		for _, arch := range entry.archs {
			if destPath, err = addAptPackage(entry.conf, entry.srcTemplate, entry.upload, arch, group.osVersion); err != nil {
				return err
			}
		}
	}

	return publishAptRepo(first.conf, destPath, group.osVersion)
}

func uploadRpmBatch(repo rpmRepo) (err error) {
	destPath := repo.files[0].destPath
	utils.Logger.Printf("[ ] Start uploading %d rpms into %s", len(repo.files), destPath)

	for _, rpm := range repo.files {
		if err = copyRpmFile(rpm); err != nil {
			return err
		}
	}

	if err = updateRpmRepo(repo.conf, repo.files[0].s3RepoPath, destPath, fmt.Sprintf("%d packages", len(repo.files))); err != nil {
		return err
	}

	utils.Logger.Printf("[✔] Uploading %d RPMs succeded for dest %s \n", len(repo.files), destPath)
	return nil
}
