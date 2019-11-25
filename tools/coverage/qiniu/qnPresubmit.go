package qiniu

import (
	"encoding/json"
	"fmt"
	"log"
	"path"
	"sort"
	"strconv"
)

const (
	//statusJSON is the JSON file that stores build success info
	statusJSON = "finished.json"

	ArtifactsDirName = "artifacts"
)

// sortBuilds converts all build from str to int and sorts all builds in descending order and
// returns the sorted slice
func sortBuilds(strBuilds []string) []int {
	var res []int
	for _, buildStr := range strBuilds {
		num, err := strconv.Atoi(buildStr)
		if err != nil {
			log.Printf("Non-int build number found: '%s'", buildStr)
		} else {
			res = append(res, num)
		}
	}
	sort.Sort(sort.Reverse(sort.IntSlice(res)))
	return res
}

type finishedStatus struct {
	Timestamp int
	Passed    bool
}

func isBuildSucceeded(jsonText []byte) bool {
	var status finishedStatus
	err := json.Unmarshal(jsonText, &status)
	return err == nil && status.Passed
}

// FindBaseProfile finds the coverage profile file from the latest healthy build
// stored in given gcs directory
func FindBaseProfileFromQiniu(qc *Client, prowJobName, covProfileName string) ([]byte, error) {
	dirOfJob := path.Join("logs", prowJobName)
	prefix := dirOfJob + "/"
	strBuilds, err := qc.ListSubDirs(prefix)
	if err != nil {
		return nil, fmt.Errorf("error listing qiniu objects, prowjob:%v, err:%v", prowJobName, err)
	}
	log.Printf("total sub dirs: %d", len(strBuilds))

	builds := sortBuilds(strBuilds)
	profilePath := ""
	for _, build := range builds {
		buildDirPath := path.Join(dirOfJob, strconv.Itoa(build))
		dirOfStatusJSON := path.Join(buildDirPath, statusJSON)

		statusText, err := qc.ReadObject(dirOfStatusJSON)
		if err != nil {
			log.Printf("Cannot read finished.json (%s) ", dirOfStatusJSON)
		} else if isBuildSucceeded(statusText) {
			artifactsDirPath := path.Join(buildDirPath, ArtifactsDirName)
			profilePath = path.Join(artifactsDirPath, covProfileName)
			break
		}
	}
	if profilePath == "" {
		return nil, fmt.Errorf("no healthy build found for job '%s' ; total # builds = %v", dirOfJob, len(builds))
	}

	log.Printf("base cover profile path: %s", profilePath)
	return qc.ReadObject(profilePath)
}
