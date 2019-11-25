package qiniu

import (
	"context"
	"fmt"
	"io/ioutil"
	"math/rand"
	"net/http"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/qiniu/api.v7/auth"
	"github.com/qiniu/api.v7/client"
	"github.com/qiniu/api.v7/storage"
	"github.com/sirupsen/logrus"
)

type Config struct {
	Bucket    string `json:"bucket"`
	AccessKey string `json:"accessKey"`
	SecretKey string `json:"secretKey"`

	// domain used to download files from qiniu cloud
	Domain string `json:"domain"`
}

// Client for the operation with qiniu cloud
type Client struct {
	cfg           *Config
	BucketManager *storage.BucketManager
}

// NewQNArtifactFetcher creates a new ArtifactFetcher
func NewClient(cfg *Config) *Client {
	return &Client{
		cfg:           cfg,
		BucketManager: storage.NewBucketManager(auth.New(cfg.AccessKey, cfg.SecretKey), nil),
	}
}

func (q *Client) GetQiniuObjectHandle(key string) *ObjectHandle {
	return &ObjectHandle{
		key:    key,
		cfg:    q.cfg,
		bm:     q.BucketManager,
		mac:    auth.New(q.cfg.AccessKey, q.cfg.SecretKey),
		client: &client.Client{Client: http.DefaultClient},
	}
}

// ReadObject to read all the content of key
func (q *Client) ReadObject(key string) ([]byte, error) {
	objectHandle := q.GetQiniuObjectHandle(key)
	reader, err := objectHandle.NewReader(context.Background())
	if err != nil {
		return nil, fmt.Errorf("error getting qiniu artifact reader: %v", err)
	}
	defer reader.Close()
	return ioutil.ReadAll(reader)
}

// ListAll to list all the files with contains the expected prefix
func (q *Client) ListAll(ctx context.Context, prefix string, delimiter string) ([]string, error) {
	var files []string
	artifacts, err := q.listEntries(prefix, delimiter)
	if err != nil {
		return files, err
	}

	for _, item := range artifacts {
		files = append(files, item.Key)
	}

	return files, nil
}

// ListAll to list all the entries with contains the expected prefix
func (q *Client) listEntries(prefix string, delimiter string) ([]storage.ListItem, error) {
	var marker string
	var artifacts []storage.ListItem

	wait := []time.Duration{16, 32, 64, 128, 256, 256, 512, 512}
	for i := 0; ; {
		entries, _, nextMarker, hashNext, err := q.BucketManager.ListFiles(q.cfg.Bucket, prefix, delimiter, marker, 500)
		if err != nil {
			logrus.WithField("prefix", prefix).WithError(err).Error("Error accessing QINIU artifact.")
			if i >= len(wait) {
				return artifacts, fmt.Errorf("timed out: error accessing QINIU artifact: %v", err)
			}
			time.Sleep((wait[i] + time.Duration(rand.Intn(10))) * time.Millisecond)
			i++
			continue
		}
		artifacts = append(artifacts, entries...)

		if hashNext {
			marker = nextMarker
		} else {
			break
		}
	}

	return artifacts, nil
}

// GetAccessUrl return a url which can access artifact directly in qiniu
func (q *Client) GetAccessUrl(key string, timeout time.Duration) string {
	deadline := time.Now().Add(timeout).Unix()
	return storage.MakePrivateURL(auth.New(q.cfg.AccessKey, q.cfg.SecretKey), q.cfg.Domain, key, deadline)

}

type LogHistoryTemplate struct {
	BucketName string
	KeyPath    string
	Items      []logHistoryItem
}

type logHistoryItem struct {
	Name string
	Size string
	Time string
	Url  string
}

// Artifacts lists all artifacts available for the given job source
func (q *Client) GetArtifactDetails(key string) (*LogHistoryTemplate, error) {
	tmpl := new(LogHistoryTemplate)
	item := logHistoryItem{}
	listStart := time.Now()
	artifacts, err := q.listEntries(key, "")
	if err != nil {
		return tmpl, err
	}

	for _, entry := range artifacts {
		item.Name = splitKey(entry.Key, key)
		item.Size = size(entry.Fsize)
		item.Time = timeConv(entry.PutTime)
		item.Url = q.GetAccessUrl(entry.Key, time.Duration(time.Second*60*60))
		tmpl.Items = append(tmpl.Items, item)
	}

	logrus.WithField("duration", time.Since(listStart).String()).Infof("Listed %d artifacts.", len(tmpl.Items))
	return tmpl, nil
}

func splitKey(item, key string) string {
	return strings.TrimPrefix(item, key)
}

func size(fsize int64) string {
	return strings.Join([]string{strconv.FormatInt(fsize, 10), "bytes"}, " ")
}

func timeConv(ptime int64) string {
	s := strconv.FormatInt(ptime, 10)[0:10]
	t, err := strconv.ParseInt(s, 10, 64)
	if err != nil {
		logrus.Errorf("time string parse int error : %v", err)
		return ""
	}
	tm := time.Unix(t, 0)
	return tm.Format("2006-01-02 03:04:05 PM")
}

func (q *Client) ListSubDirs(prefix string) ([]string, error) {
	var dirs []string
	var marker string

	wait := []time.Duration{16, 32, 64, 128, 256, 256, 512, 512}
	for i := 0; ; {
		// use rsf list v2 interface to get the sub folder based on the delimiter
		entries, err := q.BucketManager.ListBucketContext(context.Background(), q.cfg.Bucket, prefix, "/", marker)
		if err != nil {
			logrus.WithField("prefix", prefix).WithError(err).Error("Error accessing QINIU artifact.")
			if i >= len(wait) {
				return dirs, fmt.Errorf("timed out: error accessing QINIU artifact: %v", err)
			}
			time.Sleep((wait[i] + time.Duration(rand.Intn(10))) * time.Millisecond)
			i++
			continue
		}

		for entry := range entries {
			if entry.Dir != "" {
				// entry.Dir should be like "logs/kodo-periodics-integration-test/1181915661132107776/"
				// the sub folder is 1181915661132107776, also known as prowjob buildid.
				buildId := getBuildId(entry.Dir)
				if buildId != "" {
					dirs = append(dirs, buildId)
				} else {
					logrus.Warnf("invalid dir format: %v", entry.Dir)
				}
			}

			marker = entry.Marker
		}

		if marker != "" {
			i = 0
		} else {
			break
		}
	}

	return dirs, nil
}

var nonPRLogsBuildIdSubffixRe = regexp.MustCompile("([0-9]+)/$")

// extract the build number from dir path
// expect the dir as the following formats:
// 1. logs/kodo-periodics-integration-test/1181915661132107776/
func getBuildId(dir string) string {
	matches := nonPRLogsBuildIdSubffixRe.FindStringSubmatch(dir)
	if len(matches) == 2 {
		return matches[1]
	}

	return ""
}
