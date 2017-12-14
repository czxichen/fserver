package fserver

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"
)

func Server(cfg *Config) error {
	if err := cfg.InitOpts(); err != nil {
		Log.Fatalf("%s\n", err.Error())
	}
	http.HandleFunc("/", cfg.ServeHTTP)
	return http.ListenAndServe(cfg.AddrAndPort, nil)
}

const (
	api_discovery   = "/_internal/api/discovery"
	api_peerStatus  = "/_internal/api/peerStatus"
	api_checkmd5sum = "/_internal/api/checkmd5sum"
	upload          = "/_internal/upload"
	redirect        = "/_internal/redirect"
	download        = "/_internal/download"
)

type Config struct {
	AddrAndPort   string    `json:"addrandport"` //监听的地址端口
	Label         string    `json:"label"`       //本节点的标识
	Data          cache     `json:"data"`        //数据缓存目录
	Peers         []Options `json:"peers"`       //节点的配置文件
	TimeOut       int64     `json:"timeout"`     //到其他节点下载的时候超时时间
	Expires       int64     `json:"expires"`     //Token刷新周期
	AllowNet      []string  `json:"allownet"`    //允许的网络地址池
	WhiteIp       []string  `json:"whiteip"`     //允许的IP地址
	closed        int32
	ctx           context.Context
	cancel        context.CancelFunc
	allownet      []*net.IPNet
	whiteip       map[string]struct{}
	token         *token //当外网访问的时候,加一个简单的验证
	lock          sync.Mutex
	peersStatus   map[string]*peer
	peersDownload map[string]context.Context
}

//初始化以后才能使用方法,不然会panic
func (cfg *Config) InitOpts() error {
	cfg.Data = cache(strings.Replace(string(cfg.Data), "\\", "/", -1))

	if cfg.Data[len(cfg.Data)-1] != '/' {
		cfg.Data = cfg.Data + "/"
	}

	info, err := os.Lstat(string(cfg.Data))
	if err != nil {
		if os.IsNotExist(err) {
			err = os.MkdirAll(string(cfg.Data), 0644)
		}
		if err != nil {
			return err
		}
	} else {
		if !info.IsDir() {
			return fmt.Errorf("Cache path must be directory")
		}
	}

	cfg.closed = 0

	if cfg.TimeOut == 0 {
		cfg.TimeOut = 7200
	}

	if cfg.Expires == 0 {
		cfg.Expires = 3600
	}
	if len(cfg.AllowNet) == 0 {
		cfg.AllowNet = []string{"10.0.0.0/8", "172.16.0.0/12", "192.168.0.0/16"}
	}

	netlist := make([]*net.IPNet, 0, len(cfg.AllowNet))
	for _, v := range cfg.AllowNet {
		_, ipnet, err := net.ParseCIDR(v)
		if err != nil {
			return fmt.Errorf("Parse %s error:%s", v, err.Error())
		}
		netlist = append(netlist, ipnet)
	}
	cfg.allownet = netlist

	ipmap := make(map[string]struct{}, len(cfg.WhiteIp)+len(cfg.Peers))
	for _, v := range cfg.WhiteIp {
		ipmap[v] = struct{}{}
	}
	cfg.whiteip = ipmap

	cfg.token = &token{expires: cfg.Expires}

	cfg.ctx, cfg.cancel = context.WithCancel(context.Background())
	go cfg.token.Run(cfg.ctx)

	cfg.peersStatus = make(map[string]*peer, len(cfg.Peers))
	cfg.peersDownload = make(map[string]context.Context)

	for _, opt := range cfg.Peers {
		if opt.Label == cfg.Label {
			return fmt.Errorf("Label %s conflict", opt.Label)
		}

		if _, ok := cfg.peersStatus[opt.Label]; ok {
			return fmt.Errorf("Label %s conflict", opt.Label)
		}

		if opt.Interval == 0 {
			opt.Interval = 10
		}

		host, _, err := net.SplitHostPort(opt.AddrAndPort)
		if err != nil {
			return fmt.Errorf("Invalid host:port %s\n", err.Error())
		}

		cfg.whiteip[host] = struct{}{}

		if !strings.HasPrefix(opt.AddrAndPort, "http://") {
			opt.AddrAndPort = "http://" + opt.AddrAndPort
		}

		pr := &peer{option: opt, status: Status_Unknow}
		cfg.peersStatus[opt.Label] = pr
		go pr.Discovery()
	}
	return nil
}

func (cfg *Config) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	Log.Infof("From %s URI:%s\n", r.RemoteAddr, r.RequestURI)
	if atomic.LoadInt32(&cfg.closed) != 0 {
		http.Error(w, "Service is closed", http.StatusServiceUnavailable)
		return
	}

	//验证是否允许访问
	if !cfg.auth(r) {
		http.Error(w, "Unauthorized", http.StatusUnauthorized)
		r.Body.Close()
		return
	}

	w.Header().Set("Server", "Fserver/1.0")
	switch r.URL.Path {
	case download:
		cfg.fileServer(w, r)
	case upload:
		cfg.upLoad(w, r)
	case redirect:
		cfg.redirect(w, r)
	case api_checkmd5sum:
		cfg.checkMd5sum(w, r)
	case api_peerStatus:
		w.Write(cfg.peerStatus()) //查看节点当前的状态
	case api_discovery:
		w.WriteHeader(http.StatusOK) //查看此节点是否能正常响应
	default:
		http.Error(w, "Access deny", http.StatusForbidden)
	}

	r.Body.Close()
}

//检查一个文件的md5sum,如果rm为真如果不一致,则会直接删除
func (cfg *Config) checkMd5sum(w http.ResponseWriter, r *http.Request) {
	filename := r.FormValue("filename")
	var md5sum string = ""
	if filename != "" {
		Log.Infof("From %s CheckMd5sum with %s\n", cfg.Label, filename)
		md5sum = cfg.Data.CheckMd5sum(filename, r.FormValue("remove") == "true")
	}
	w.WriteHeader(http.StatusOK)
	w.Write([]byte(md5sum))

	r.Body.Close()
}

//如果本地不存在,则遍历其他节点
func (cfg *Config) fileServer(w http.ResponseWriter, r *http.Request) {
	defer r.Body.Close()

	filename := r.FormValue("filename")
	if filename != "" {
		err := cfg.sendFile(w, filename)
		if err == nil {
			return
		}
		if os.IsNotExist(err) {
			if r.FormValue("redirect") == "true" {
				//只做跳转不做缓存
				if t, h := cfg.peerRedirect(filename); t != "" {
					http.Redirect(w, r, fmt.Sprintf("%s%s&token=%s", h, r.RequestURI, t), http.StatusFound)
				} else {
					http.NotFound(w, r)
				}
				return
			}
			ctx := cfg.peerDownload(filename)
			select {
			case <-ctx.Done():
			}
			if ctx.Err() == context.Canceled {
				err = cfg.sendFile(w, filename)
				if err == nil {
					return
				}
			}
		} else {
			Log.Warnf("FileServer error:%s\n", err.Error())
		}
	}
	http.NotFound(w, r)
}

//查看本机是否存在某文件,如果存在,则响应200附加token
func (cfg *Config) redirect(w http.ResponseWriter, r *http.Request) {
	defer r.Body.Close()

	if filename := r.FormValue("filename"); filename != "" {
		if cfg.Data.IsExist(filename) {
			w.Header().Set("token", cfg.token.Get())
			w.WriteHeader(http.StatusOK)
			return
		}
	}
	http.NotFound(w, r)
}

//处理上传的文件
func (cfg *Config) upLoad(w http.ResponseWriter, r *http.Request) {
	defer r.Body.Close()
	filename := r.FormValue("filename")
	if filename != "" {
		err := cfg.Data.WirteFile(filename, io.LimitReader(r.Body, r.ContentLength))
		if err == nil {
			w.WriteHeader(http.StatusOK)
		} else {
			http.Error(w, err.Error(), http.StatusInternalServerError)
		}
		return
	}
	http.Error(w, "filename field must be submitted", http.StatusForbidden)
}

func (cfg *Config) Close() {
	atomic.StoreInt32(&cfg.closed, 1)
	cfg.cancel()
}

//发送指定的文件
func (cfg *Config) sendFile(w http.ResponseWriter, filename string) error {
	read, info, err := readFile(cfg.Data.String() + filename)
	if err == nil {
		w.Header().Set("Content-Type", "application/octet-stream")
		w.Header().Set("Content-Disposition", "attachment; filename="+filename)
		w.Header().Set("Content-Length", strconv.FormatInt(info.Size(), 10))
		_, err = io.Copy(w, read)
		read.Close()
	}
	return err
}

//查看那个节点上有要下载的文件
func (cfg *Config) peerRedirect(filename string) (string, string) {
	for _, pr := range cfg.peersStatus {
		if t := pr.Redirect(filename); t != "" {
			return t, pr.option.AddrAndPort
		}
	}
	return "", ""
}

//本地不存在就去其他节点看是否存在,如果存在则先在本地缓存再响应给客户端
func (cfg *Config) peerDownload(filename string) context.Context {
	cfg.lock.Lock()
	ctx, ok := cfg.peersDownload[filename]
	if ok {
		cfg.lock.Unlock()
		return ctx
	}
	ctx, cancel := context.WithTimeout(cfg.ctx, time.Duration(cfg.TimeOut)*time.Second)
	cfg.peersDownload[filename] = ctx
	cfg.lock.Unlock()

	go func() {
		Log.Infof("From peers download %s\n", filename)
		for _, pr := range cfg.peersStatus {
			if pr.Download(filename, cfg.Data.WirteFile) {
				break
			}
		}
		cancel() //通知等待的客户端已经缓存结束,缓存结束不代表一定是缓存成功.
		cfg.lock.Lock()
		delete(cfg.peersDownload, filename)
		cfg.lock.Unlock()
	}()
	return ctx
}

//查看每个节点的状态,以json格式返回,并不会及时刷新每个节点的状态,只有主动请求的时候,报错才会刷新节点状态
func (cfg *Config) peerStatus() []byte {
	if cfg.peersStatus == nil {
		return []byte("{}")
	}
	var peers = make([]string, 0, len(cfg.peersStatus))

	buf := bytes.NewBuffer(nil)
	buf.WriteString("{")
	for k, v := range cfg.peersStatus {
		peers = append(peers, fmt.Sprintf(`"%s":%s`, k, v.String()))
	}
	buf.WriteString(strings.Join(peers, ","))
	buf.WriteString("}")
	return buf.Bytes()
}

//如果含有有效的token则认证成功,不包含token的请求则会根据白名单和网段验证
func (cfg *Config) auth(r *http.Request) bool {
	if t := r.Header.Get("token"); t != "" {
		if cfg.token.Check(t) {
			return true
		}
	} else {
		ip, _, err := net.SplitHostPort(r.RemoteAddr)
		if err != nil {
			return false
		}

		if _, ok := cfg.whiteip[ip]; ok {
			return true
		}

		ipnet := net.ParseIP(ip)
		for _, v := range cfg.allownet {
			if v.Contains(ipnet) {
				return true
			}
		}
	}
	return false
}
