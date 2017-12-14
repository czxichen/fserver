package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"net/http"
	"os"
	"strings"

	"github.com/czxichen/command/parse"
	"github.com/czxichen/fserver"
)

var (
	GitHash string
	Version string

	hostport string //指定远端的地址端口
	filename string //指定要上传的文件路径
	cfgPath  string //指定配置文件路径
	runType  string // server or client
	cfgExmp  bool   //打印配置样例
	version  bool   //打印版本信息
)

func init() {
	flag.BoolVar(&version, "v", false, "-v 打印版本信息")
	flag.BoolVar(&cfgExmp, "e", false, "-e 打印配置样例")
	flag.StringVar(&runType, "type", "", "-type server|client 指定运行的类型")
	flag.StringVar(&cfgPath, "cfg", "fserver.json", "-cfg fserver.json 指定配置文件,runType为server的时候有效")
	flag.StringVar(&hostport, "host", "", "-host 指定远程主机地址和端口,runType为client的时候有效")
	flag.StringVar(&filename, "path", "", "-path 指定要上传的文件路径,runType为client的时候有效")
	flag.Parse()
}

func main() {
	if version {
		fmt.Printf("Author:czxichen@163.com\nGitHash:%s\nVersion:%s\n", GitHash, Version)
		return
	}
	if cfgExmp {
		example()
		return
	}

	if runType == "" {
		flag.PrintDefaults()
		return
	}

	runType = strings.ToLower(runType)
	switch runType {
	case "server":
		var cfg = new(fserver.Config)
		err := config(cfgPath, cfg)
		if err != nil {
			fmt.Fprintf(os.Stderr, "Parse config error:%s\n", err.Error())
			return
		}
		err = cfg.InitOpts()
		if err != nil {
			fmt.Fprintf(os.Stderr, "InitOpts error:%s\n", err.Error())
			return
		}
		err = http.ListenAndServe(cfg.AddrAndPort, cfg)
		if err != nil {
			fmt.Printf("Listen error:%s\n", err.Error())
		}
	case "client":
		if !strings.HasPrefix(hostport, "http://") {
			hostport = "http://" + hostport
		}
		err := fserver.Upload(filename, hostport)
		if err != nil {
			fmt.Printf("Upload file error:%s\n", err.Error())
			return
		}
	default:
		fmt.Fprintf(os.Stderr, "-type must be server or client")
	}
}

func config(path string, cfg *fserver.Config) error {
	buf, err := parse.Parse(path, "#")
	if err != nil {
		return err
	}
	return json.Unmarshal(buf, cfg)
}

func example() {
	const cfgstr = `
{
	"addrandport":"127.0.0.1:1789",	#设置本服务监听的地址端口
	"label":"机房A",	#为本服务加一个标签
	"data":"/data/cache",	#设置文件缓存的路径
	"peers":[	#设置其他节点,可以为空,用来查找本地不存在的文件
		{"addrandport":"127.0.0.1:1790","label":"机房","interval":10}
	],
	"timeout":7200,	#设置从其他节点下载文件时超时时间
	"expires":1200,	#设置随机Token的失效时间,用来做简单的验证,如果Token有效则不做网段和白名单限制
	"allownet":[	#允许访问的网段,如果为空默认为["10.0.0.0/8","172.16.0.0/12","192.168.0.0/16"]
		"10.0.0.0/8"
	],
	"whiteip":[	#允许白名单
		"172.18.80.247"
	]
}
`
	fmt.Printf(cfgstr)
}
