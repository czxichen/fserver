package fserver

import (
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"sync"
	"time"
)

type peer struct {
	sync.RWMutex
	status  Status
	tagging string
	option  Options
}

func (pr *peer) String() string {
	pr.RLock()
	defer pr.RUnlock()
	return fmt.Sprintf(`{"status":"%s","tagging":"%s"}`, pr.status.String(), pr.tagging)
}

func (pr *peer) Status() Status {
	pr.RLock()
	defer pr.RUnlock()
	return pr.status
}

func (pr *peer) Discovery() {
	var (
		tagging string
		trick   = time.Second * time.Duration(pr.option.Interval)
	)
	for {
		resp, err := http.Get(pr.option.AddrAndPort + api_discovery)
		if err == nil {
			if resp.StatusCode == http.StatusOK {
				Log.Infof("Discovery %s successfully\n", pr.option.Label)
			} else {
				tagging = fmt.Sprintf("Discovery %s error:%s\n", pr.option.Label, resp.Status)
			}
		} else {
			if _, ok := err.(*net.OpError); !ok {
				Log.Warnf("Discovery %s error:%s\n", pr.option.Label, err.Error())
				time.Sleep(trick)
				continue
			}
			tagging = fmt.Sprintf("Discovery %s error:%s\n", pr.option.Label, err.Error())
		}
		resp.Body.Close()

		pr.Lock()
		pr.tagging = tagging
		if tagging == "" {
			pr.status = Status_OnLine
		} else {
			pr.status = Status_Unknow
		}
		pr.Unlock()
		break
	}
}

func (pr *peer) Redirect(filename string) string {
	if pr.Status() != Status_OnLine {
		Log.Warnf("From %s redirect %s is offline\n", pr.option.Label, filename)
		return ""
	}
	resp, err := pr.do(filename, redirect)
	if err == nil {
		if resp.StatusCode == http.StatusOK {
			return resp.Header.Get("token")
		}
	}
	return ""
}

func (pr *peer) Download(filename string, WirteFile func(string, io.Reader) error) bool {
	if pr.Status() != Status_OnLine {
		Log.Warnf("From %s download %s is offline\n", pr.option.Label, filename)
		return false
	}
	resp, err := pr.do(filename, download)
	if err != nil {
		return false
	}
	defer resp.Body.Close()
	if resp.StatusCode == http.StatusOK {
		err = WirteFile(filename, io.LimitReader(resp.Body, resp.ContentLength))
		if err == nil {
			Log.Infof("From %s download %s successful\n", pr.option.Label, filename)
			return true
		} else {
			Log.Errorf("From %s download error:%s\n", pr.option.Label, err.Error())
		}
	} else {
		Log.Warnf("From %s download error:%s\n", pr.option.Label, resp.Status)
	}
	return false
}

func (pr *peer) do(filename, uri string) (*http.Response, error) {
	resp, err := http.Get(fmt.Sprintf("%s%s?filename=%s", pr.option.AddrAndPort, uri, filename))
	if err != nil {
		Log.Warnf("From %s %s %s error:%s\n", pr.option.Label, uri, filename, err.Error())
		if uerr, ok := err.(*url.Error); ok {
			if _, ok = uerr.Err.(*net.OpError); ok {
				pr.Lock()
				pr.status = Status_OffLine
				pr.Unlock()
				go pr.Discovery()
			}
		}
	}
	return resp, err
}
