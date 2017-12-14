package fserver

import (
	"context"
	"crypto/md5"
	"encoding/hex"
	"fmt"
	"io"
	"log"
	"math/rand"
	"os"
	"time"
)

var Log Logger = logger{}

type Logger interface {
	Infof(format string, v ...interface{})
	Warnf(format string, v ...interface{})
	Errorf(format string, v ...interface{})
	Fatalf(format string, v ...interface{})
}

var _Status = [3]string{"Unknow", "OffLine", "OnLine"}

type Status int

func (s Status) String() string {
	if s > 2 {
		return ""
	}
	return _Status[s]
}

const (
	Status_Unknow Status = iota
	Status_OffLine
	Status_OnLine
)

type logger struct{}

func (logger) Infof(format string, v ...interface{})  { log.Printf("[Info] "+format, v...) }
func (logger) Warnf(format string, v ...interface{})  { log.Printf("[Warn] "+format, v...) }
func (logger) Errorf(format string, v ...interface{}) { log.Printf("[Error] "+format, v...) }
func (logger) Fatalf(format string, v ...interface{}) { log.Fatalf("[Fatal] "+format, v...) }

type cache string

func (c cache) String() string {
	return string(c)
}

func (c cache) ReadFile(path string) (io.ReadCloser, os.FileInfo, error) {
	return readFile(c.String() + path)
}

func (c cache) WirteFile(path string, read io.Reader) error {
	FilePath := c.String() + path
	_, err := os.Lstat(FilePath)
	if err == nil {
		return nil
	}

	File, err := os.OpenFile(FilePath+".tmp", os.O_CREATE|os.O_RDWR|os.O_EXCL, 0644)
	if err != nil {
		if os.IsExist(err) {
			err = fmt.Errorf("Already transporting wait a moment retry")
		}
		return err
	}

	_, err = io.Copy(File, read)
	File.Close()
	if err == nil {
		os.Rename(FilePath+".tmp", FilePath)
	} else {
		os.Remove(FilePath + ".tmp")
	}
	return err
}

//查看本地是否存在对应的文件
func (c cache) IsExist(path string) bool {
	_, err := os.Lstat(c.String() + path)
	return err == nil
}

//校验对应文件的md5是否一致,如果不一致,rm为真,则会删除,否则修改成一致
func (c cache) CheckMd5sum(path string, rm bool) string {
	md5sum := fileMd5(c.String() + path)
	match := md5sum == path
	if rm && !match {
		os.Remove(c.String() + path)
	} else {
		if !match {
			os.Rename(c.String()+path, c.String()+md5sum)
		}
	}
	return md5sum
}

func readFile(path string) (io.ReadCloser, os.FileInfo, error) {
	File, err := os.Open(path)
	if err != nil {
		return nil, nil, err
	}
	info, err := File.Stat()
	if err != nil {
		File.Close()
		return nil, nil, err
	}
	return File, info, nil
}

func fileMd5(path string) (str string) {
	File, err := os.Open(path)
	if err != nil {
		return ""
	}
	str = readMd5(File)
	File.Close()
	return
}

func readMd5(r io.Reader) string {
	h := md5.New()
	io.Copy(h, r)
	return hex.EncodeToString(h.Sum(nil))
}

type Options struct {
	AddrAndPort string `json:"addrandport"`
	Label       string `json:"label"`
	Interval    int64  `json:"interval"`
}

type token struct {
	expires int64
	last    int64
	old     string
	new     string
}

func (T *token) Run(ctx context.Context) {
	T.update(16)

	var tick = time.Tick(time.Duration(T.expires) * time.Second)
	for {
		select {
		case <-ctx.Done():
			return
		case <-tick:
			T.update(16)
		}
	}
}

func (T *token) Check(str string) bool {
	if str == T.new {
		return true
	}
	if time.Now().Unix()-T.last <= 300 {
		if str == T.old {
			return true
		}
	}
	return false
}

func (T *token) Get() string {
	return T.new
}

const base = "0123456789abcdefghijklmnopqrstuvwxyz"

func (T *token) update(l int) {
	randstr := make([]byte, l)
	for i := 0; i < l; i++ {
		randstr[i] = base[rand.Intn(36)]
	}

	if T.new != "" {
		T.old = T.new
	}
	T.new = string(randstr)
	if T.old == "" {
		T.old = T.new
	}

	T.last = time.Now().Unix()
}
