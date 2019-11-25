/* Copyright 2018 The Knative Authors.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package main

import (
	"encoding/json"
	"flag"
	"io/ioutil"
	"log"
	"os"

	"github.com/sirupsen/logrus"
	"knative.dev/test-infra/tools/coverage/artifacts"
	"knative.dev/test-infra/tools/coverage/githubUtil/githubPr"
	"knative.dev/test-infra/tools/coverage/logUtil"
	"knative.dev/test-infra/tools/coverage/qiniu"
	"knative.dev/test-infra/tools/coverage/testgrid"
)

const (
	keyCovProfileFileName    = "key-cover-profile.txt"
	defaultStdoutRedirect    = "stdout.txt"
	defaultCoverageTargetDir = "."
	defaultGcsBucket         = "knative-prow"
	defaultPostSubmitJobName = ""
	defaultCovThreshold      = 50
	//defaultArtifactsDir        = "./artifacts/"
	defaultCoverageProfileName = "coverage_profile.txt"
)

func main() {
	// Enable line numbers in logging
	log.SetFlags(log.LstdFlags | log.Lshortfile)
	log.Println("entering code coverage main")

	postSubmitJobName := flag.String("postsubmit-job-name", defaultPostSubmitJobName, "name of the prow job")
	coverageTargetDir := flag.String("cov-target", defaultCoverageTargetDir, "target directory for test coverage")
	localCoverageProfile := flag.String("local-profile", defaultCoverageProfileName, "local coverage profile to analyze")
	githubTokenPath := flag.String("github-token", "", "path to token to access github repo")
	covThreshold := flag.Int("cov-threshold-percentage", defaultCovThreshold, "token to access GitHub repo")
	postingBotUserName := flag.String("posting-robot", "qiniu-bot", "github user name for coverage robot")
	remoteProfileName := flag.String("remote-profile-name", "filtered.cov", "code coverage profile file name in cloud")
	qiniuConfig := flag.String("qiniu-credential", "", "path to credential file to access qiniu cloud")
	flag.Parse()

	log.Printf("container flag list:  postSubmitJobName=%s; "+
		"cov-target=%s; profile-name=%s; github-token=%s; "+
		"cov-threshold-percentage=%d; posting-robot=%s;",
		*postSubmitJobName, *coverageTargetDir, *localCoverageProfile,
		*githubTokenPath, *covThreshold, *postingBotUserName)

	log.Println("Getting env values")
	pr := os.Getenv("PULL_NUMBER")
	pullSha := os.Getenv("PULL_PULL_SHA")
	baseSha := os.Getenv("PULL_BASE_SHA")
	repoOwner := os.Getenv("REPO_OWNER")
	repoName := os.Getenv("REPO_NAME")
	jobType := os.Getenv("JOB_TYPE")
	jobName := os.Getenv("JOB_NAME")
	buildStr := os.Getenv("BUILD_NUMBER")

	log.Printf("Running coverage for PR=%s; PR commit SHA = %s;base SHA = %s", pr, pullSha, baseSha)

	localArtifacts := artifacts.NewLocalArtifacts(
		os.Getenv("ARTIFACTS"), //需要保存本次覆盖率结果(包括html)到远端
		*localCoverageProfile,
		keyCovProfileFileName,
		defaultStdoutRedirect,
	)

	log.Printf("Running workflow: %s\n", jobType)
	switch jobType {
	case "periodic":
		log.Printf("job type is %v, producing testsuite xml...\n", jobType)
		testgrid.ProfileToTestsuiteXML(localArtifacts, *covThreshold)
	case "presubmit":
		if *qiniuConfig == "" {
			logUtil.LogFatalf("qiniu credential file must be provided")
		}

		var qc *qiniu.Client
		var conf qiniu.Config

		files, err := ioutil.ReadFile(*qiniuConfig)
		if err != nil {
			logrus.WithError(err).Fatal("Error reading qiniu config file")
		}

		if err := json.Unmarshal(files, &conf); err != nil {
			logrus.WithError(err).Fatal("Error unmarshall qiniu config file")
		}

		if conf.Bucket == "" {
			logrus.WithError(err).Fatal("no qiniu bucket provided")
		}

		if conf.AccessKey == "" || conf.SecretKey == "" {
			logrus.WithError(err).Fatal("either qiniu access key or secret key was not provided")
		}

		if conf.Domain == "" {
			logrus.WithError(err).Fatal("no qiniu bucket domain was provided")
		}
		qc = qiniu.NewClient(&conf)

		prData := githubPr.New(*githubTokenPath, repoOwner, repoName, pr, *postingBotUserName)

		entry := PreSubmitEntry{
			PostSubmitJob:          *postSubmitJobName,
			PostSubmitCoverProfile: *remoteProfileName,

			Org:     repoOwner,
			Repo:    repoName,
			BuildId: buildStr,
			PR:      pr,
			JobName: jobName,

			qc:     qc,
			github: prData,
		}

		isCoverageLow, err := entry.RunPresubmit(localArtifacts)
		if isCoverageLow {
			logUtil.LogFatalf("Code coverage is below threshold (%d%%), "+
				"fail presubmit workflow intentionally", *covThreshold)
		}
		if err != nil {
			log.Fatal(err)
		}

	case "postsubmit":
		log.Printf("job type %s, do nothing", jobType)
	}

	log.Println("end of code coverage main")
}
