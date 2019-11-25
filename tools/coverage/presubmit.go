/*
Copyright 2019 The Knative Authors

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    https://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package main

import (
	"bytes"
	"fmt"
	"io/ioutil"
	"log"
	"path"
	"strconv"
	"time"

	"knative.dev/test-infra/tools/coverage/artifacts"
	"knative.dev/test-infra/tools/coverage/calc"
	"knative.dev/test-infra/tools/coverage/githubUtil"
	"knative.dev/test-infra/tools/coverage/githubUtil/githubPr"
	"knative.dev/test-infra/tools/coverage/io"
	"knative.dev/test-infra/tools/coverage/line"
	"knative.dev/test-infra/tools/coverage/logUtil"
	"knative.dev/test-infra/tools/coverage/qiniu"
)

type PreSubmitEntry struct {
	PostSubmitJob          string
	PostSubmitCoverProfile string
	CovThreshold           int

	Org     string
	Repo    string
	PR      string
	JobName string
	BuildId string
	qc      *qiniu.Client
	github  *githubPr.GithubPr
}

func (entry *PreSubmitEntry) JobPrefixOnQiniu() string {
	return path.Join("pr-logs", "pull", entry.Org+"_"+entry.Repo, entry.PR, entry.JobName, entry.BuildId)
}

func (entry *PreSubmitEntry) HtmlProfile() string {
	htmlProfile := fmt.Sprintf("%s-%s-pr%s-coverage.html", entry.Org, entry.Repo, entry.PR)
	return path.Join(entry.JobPrefixOnQiniu(), "artifacts", htmlProfile)
}

func (entry *PreSubmitEntry) GenerateLineCovLinks(g *calc.CoverageList) {
	calc.SortCoverages(*g.Group())
	for i := 0; i < len(*g.Group()); i++ {
		qnKey := entry.HtmlProfile() + "#file" + strconv.Itoa(i)
		authQnKey := entry.qc.GetAccessUrl(qnKey, time.Hour*24*7)
		g.Item(i).SetLineCovLink(authQnKey)
		log.Printf("g.Item(i=%d).LineCovLink(): %s\n", i, g.Item(i).LineCovLink())
	}
}

// RunPresubmit runs the pre-submit procedure
func (entry *PreSubmitEntry) RunPresubmit(arts *artifacts.LocalArtifacts) (bool, error) {
	log.Println("starting PreSubmit.RunPresubmit(...)")

	// concerned files is a collection of all the files whose coverage change will be reported
	var concernedFiles map[string]bool

	if entry.github != nil {
		concernedFiles = githubUtil.GetConcernedFiles(entry.github, "")
		if len(concernedFiles) == 0 {
			log.Printf("List of concerned committed files is empty, " +
				"don't need to run coverage profile in presubmit\n")
			return false, nil
		}
	}

	// filter the local cover profile base on files in PR list
	gNew := calc.CovList(arts.ProfileReader(), arts.KeyProfileCreator(), concernedFiles, entry.CovThreshold)

	// generate html page for the local filtered cover profile
	err := line.CreateLineCovFile(arts)

	// generate the hyperlink for the local filtered cover profile
	//line.GenerateLineCovLinks(p, gNew)
	entry.GenerateLineCovLinks(gNew)

	//// find the remote healthy cover profile
	//base := cloud.NewPostSubmit(p.Ctx, p.Client, p.Bucket,
	//	p.PostSubmitJob, cloud.ArtifactsDirNameOnGcs, arts.ProfileName())

	remoteProfile, err := qiniu.FindBaseProfileFromQiniu(entry.qc, entry.PostSubmitJob, entry.PostSubmitCoverProfile)
	if err != nil {
		logUtil.LogFatalf("failed to get remote cover profile, err:%v", err)
	}

	remoteProfileReader := artifacts.NewProfileReader(ioutil.NopCloser(bytes.NewReader(remoteProfile)))
	// filter the remote cover profile base on files in PR list
	gBase := calc.CovList(remoteProfileReader, nil, concernedFiles, entry.CovThreshold)

	// calculate the coverage delta between local and remote
	changes := calc.NewGroupChanges(gBase, gNew)

	// construct the content for github post
	postContent, isEmpty, isCoverageLow := changes.ContentForGithubPost(concernedFiles)

	io.Write(&postContent, arts.Directory(), "bot-post")

	if !isEmpty && entry.github != nil {
		err = entry.github.CleanAndPostComment(postContent)
	}

	log.Println("completed PreSubmit.RunPresubmit(...)")
	return isCoverageLow, err
}

// RunPresubmit runs the pre-submit procedure
//func RunPresubmit(p *cloud.PreSubmit, arts *artifacts.LocalArtifacts, qc *qiniu.Client) (bool, error) {
//	log.Println("starting PreSubmit.RunPresubmit(...)")
//
//	// concerned files is a collection of all the files whose coverage change will be reported
//	var concernedFiles map[string]bool
//
//	if p.GithubClient != nil {
//		concernedFiles = githubUtil.GetConcernedFiles(&p.GithubPr, "")
//		if len(concernedFiles) == 0 {
//			log.Printf("List of concerned committed files is empty, " +
//				"don't need to run coverage profile in presubmit\n")
//			return false, nil
//		}
//	}
//
//	// filter the local cover profile base on files in PR list
//	gNew := calc.CovList(arts.ProfileReader(), arts.KeyProfileCreator(), concernedFiles, p.CovThreshold)
//
//	// generate html page for the local filtered cover profile
//	err := line.CreateLineCovFile(arts)
//
//	// generate the hyperlink for the local filtered cover profile
//	line.GenerateLineCovLinks(p, gNew)
//
//	//// find the remote healthy cover profile
//	//base := cloud.NewPostSubmit(p.Ctx, p.Client, p.Bucket,
//	//	p.PostSubmitJob, cloud.ArtifactsDirNameOnGcs, arts.ProfileName())
//
//	cloud.FindBaseProfileFromQiniu(qc)
//
//	// filter the remote cover profile base on files in PR list
//	gBase := calc.CovList(base.ProfileReader(), nil, concernedFiles, p.CovThreshold)
//
//	// calculate the coverage delta between local and remote
//	changes := calc.NewGroupChanges(gBase, gNew)
//
//	// construct the content for github post
//	postContent, isEmpty, isCoverageLow := changes.ContentForGithubPost(concernedFiles)
//
//	io.Write(&postContent, arts.Directory(), "bot-post")
//
//	if !isEmpty && p.GithubClient != nil {
//		err = p.GithubPr.CleanAndPostComment(postContent)
//	}
//
//	log.Println("completed PreSubmit.RunPresubmit(...)")
//	return isCoverageLow, err
//}
