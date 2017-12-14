package fserver

import (
	"fmt"
	"io"
	"io/ioutil"
	"net/http"
	"os"
)

func Upload(path, url string) error {
	File, err := os.Open(path)
	if err != nil {
		return err
	}

	defer File.Close()
	if md5sum := readMd5(File); md5sum != "" {
		File.Seek(0, 0)
		info, err := File.Stat()
		if err != nil {
			return err
		}
		req, err := http.NewRequest("POST", fmt.Sprintf("%s%s%s?filename=%s", url, upload, md5sum), File)
		if err != nil {
			return err
		}
		req.ContentLength = info.Size()
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			return err
		}
		defer resp.Body.Close()
		if resp.StatusCode == http.StatusOK {
			Log.Infof("Filename:%s\n", md5sum)
			return nil
		}
		buf, err := ioutil.ReadAll(resp.Body)
		if err != nil {
			return err
		}
		return fmt.Errorf("%s", buf)
	}
	return fmt.Errorf("Md5sum error:%s", err.Error())
}

func Download(filename, url string, w io.Writer) error {
	resp, err := http.Get(fmt.Sprintf("%s%s?filename=%s", url, download, filename))
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode == http.StatusOK {
		_, err = io.Copy(w, io.LimitReader(resp.Body, resp.ContentLength))
		if err != nil {
			return err
		}
	} else {
		buf, err := ioutil.ReadAll(resp.Body)
		if err != nil {
			return err
		}
		return fmt.Errorf("%s", buf)
	}
	return nil
}
