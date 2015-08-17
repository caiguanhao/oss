package main

import (
	"bytes"
	"crypto/hmac"
	"crypto/md5"
	"crypto/sha1"
	"encoding/base64"
	"errors"
	"flag"
	"fmt"
	"io/ioutil"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"sync"
	"time"
)

type httpResponse http.Response

func (resp *httpResponse) GetResponse() (*string, error) {
	var response bytes.Buffer

	response.WriteString(fmt.Sprintf("%s %s\n", resp.Proto, resp.Status))
	for key, values := range resp.Header {
		for _, value := range values {
			response.WriteString(fmt.Sprintf("%s: %s\n", key, value))
		}
	}

	body, respErr := ioutil.ReadAll(resp.Body)
	if respErr != nil {
		return nil, respErr
	}
	defer resp.Body.Close()
	if len(body) > 0 {
		response.WriteByte('\n')
		response.Write(body)
	}

	ret := response.String()
	return &ret, nil
}

func md5hash(file []byte) []byte {
	md5sum := md5.New()
	md5sum.Write(file)
	return md5sum.Sum(nil)
}

func request(method, remotePath string, localFile []byte, localFileMD5 []byte) (*httpResponse, error) {
	req, reqErr := http.NewRequest(method, api+remotePath, bytes.NewReader(localFile))
	if reqErr != nil {
		return nil, reqErr
	}

	var md5sum string
	if localFileMD5 != nil {
		md5sum = base64.StdEncoding.EncodeToString(localFileMD5)
	} else {
		md5sum = base64.StdEncoding.EncodeToString(md5hash(localFile))
	}
	contentType := http.DetectContentType(localFile)
	date := time.Now().UTC().Format("Mon, 02 Jan 2006 15:04:05 GMT")
	path := fmt.Sprintf("/%s%s", bucket, remotePath)
	msg := strings.Join([]string{method, md5sum, contentType, date, path}, "\n")
	mac := hmac.New(sha1.New, SECRET)
	mac.Write([]byte(msg))
	signature := base64.StdEncoding.EncodeToString(mac.Sum(nil))
	auth := fmt.Sprintf("OSS %s:%s", KEY, signature)

	req.Header.Set("Authorization", auth)
	req.Header.Set("Content-MD5", md5sum)
	req.Header.Set("Content-Type", contentType)
	req.Header.Set("Date", date)

	client := &http.Client{}
	resp, respErr := client.Do(req)
	if respErr != nil {
		return nil, respErr
	}
	response := httpResponse(*resp)
	return &response, nil
}

func getHeader(remotePath, headerName string) (*string, error) {
	resp, reqErr := request("HEAD", remotePath, nil, nil)
	if reqErr != nil {
		return nil, reqErr
	}

	value := resp.Header.Get(headerName)
	return &value, nil
}

func upload(remotePath, localPath string, checkETag bool) (*string, error) {
	if dryrun {
		if verbose {
			fmt.Println(localPath, "->", api+remotePath)
		} else {
			fmt.Println(localPath, "->", remotePath)
		}
		return nil, nil
	}

	if verbose {
		fmt.Println(remotePath, "- added")
	}

	localFile, readErr := ioutil.ReadFile(localPath)
	if readErr != nil {
		return nil, readErr
	}

	var localFileMD5 []byte
	if checkETag {
		var localMD5, remoteMD5 string
		var wg sync.WaitGroup
		wg.Add(2)
		go func() {
			localFileMD5 = md5hash(localFile)
			localMD5 = fmt.Sprintf("%x", localFileMD5)
			wg.Done()
		}()
		go func() {
			etag, headErr := getHeader(remotePath, "Etag")
			if headErr == nil {
				remoteMD5 = strings.ToLower(strings.Replace(*etag, "\"", "", -1))
			}
			wg.Done()
		}()
		wg.Wait()
		if localMD5 != "" && localMD5 == remoteMD5 {
			fmt.Println(remotePath, "- no changes, ignored")
			return nil, nil
		}
	}

	fmt.Println(remotePath, "- uploading")
	resp, reqErr := request("PUT", remotePath, localFile, localFileMD5)
	if reqErr != nil {
		return nil, reqErr
	}

	response, respErr := resp.GetResponse()
	if respErr != nil {
		return nil, respErr
	}
	if resp.StatusCode != 200 {
		fmt.Println(remotePath, "- fail -", resp.Status)
		return response, nil
	}
	fmt.Println(remotePath, "- done")

	return response, nil
}

type result struct {
	path     *string
	response *string
	err      error
}

func process(done <-chan struct{}, paths <-chan string, c chan<- result) {
	for path := range paths {
		ret, err := upload(remoteRoot+path, path, true)
		select {
		case c <- result{&path, ret, err}:
		case <-done:
			return
		}
	}
}

func walkFiles(done <-chan struct{}, root string) (<-chan string, <-chan error) {
	paths := make(chan string)
	errc := make(chan error, 1)
	go func() {
		defer close(paths)
		errc <- filepath.Walk(root, func(path string, info os.FileInfo, err error) error {
			if err != nil {
				return err
			}
			if !info.Mode().IsRegular() {
				return nil
			}
			select {
			case paths <- path:
			case <-done:
				return errors.New("walk canceled")
			}
			return nil
		})
	}()
	return paths, errc
}

var api string
var bucket string
var remoteRoot string

var dryrun bool
var verbose bool

func init() {
	flag.BoolVar(&dryrun, "d", false, "")
	flag.StringVar(&bucket, "b", string(DEFAULT_BUCKET), "")
	flag.StringVar(&remoteRoot, "p", string(DEFAULT_ROOT), "")
	flag.BoolVar(&verbose, "v", false, "")

	api = fmt.Sprintf("https://%s.oss-cn-hangzhou.aliyuncs.com", bucket)

	flag.Usage = func() {
		fmt.Println("oss [OPTION] [FILE]")
		fmt.Println()
		fmt.Println("If no file is specified, current directory is used.")
		fmt.Println()
		fmt.Println("Options:")
		fmt.Println("    -b <name>  Specify bucket other than:", string(DEFAULT_BUCKET))
		fmt.Println("    -p <path>  Specify remote root directory other than:", string(DEFAULT_ROOT))
		fmt.Println()
		fmt.Println("    -v         Be verbosive")
		fmt.Println("    -d         Dry-run. See list of files that will be transferred,")
		fmt.Println("               show full URL if -v is also set")
		fmt.Println()
		fmt.Println("Built with key ID:", string(KEY))
		fmt.Println("API:", api)
		fmt.Println("Source: https://github.com/caiguanhao/oss")
	}

	flag.Parse()

	remoteRoot = regexp.MustCompile("/{2,}").ReplaceAllLiteralString(remoteRoot, "/")
	if !strings.HasSuffix(remoteRoot, "/") {
		remoteRoot += "/"
	}
	if !strings.HasPrefix(remoteRoot, "/") {
		remoteRoot = "/" + remoteRoot
	}
}

func main() {
	root := flag.Arg(0)
	if root == "" {
		root = "."
	}

	done := make(chan struct{})
	defer close(done)

	paths, errc := walkFiles(done, root)

	c := make(chan result)
	var wg sync.WaitGroup
	const concurrency = 2
	wg.Add(concurrency)
	for i := 0; i < concurrency; i++ {
		go func() {
			process(done, paths, c)
			wg.Done()
		}()
	}
	go func() {
		wg.Wait()
		close(c)
	}()

	for r := range c {
		if verbose && r.response != nil {
			path := r.path
			ret := r.response
			fmt.Println(*path, "returned:")
			fmt.Println(*ret)
		}
	}

	if err := <-errc; err != nil {
		fmt.Println(err)
	}
}
