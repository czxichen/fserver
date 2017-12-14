[![Build Status](https://travis-ci.org/czxichen/work-stacks.svg?branch=master)](https://travis-ci.org/czxichen/work-stacks)

# Install fserver
* 
**git clone https://github.com/czxichen/fserver.git**
    
   ** make build version=1.0.0**

* 
**go build -i -ldflags "-s -w" github.com/czxichen/fserver/cmd **
***
# Usage fserver

查看帮助


```
fserver -h
  -cfg string
        -cfg fserver.json 指定配置文件,runType为server的时候有效 (default "fserver.json")
  -e    
    	-e 打印配置样例
  -host string
        -host 指定远程主机地址和端口,runType为client的时候有效
  -path string
        -path 指定要上传的文件路径,runType为client的时候有效
  -type string
        -type server|client 指定运行的类型
  -v    -v 打印版本信息
```

命令参考
```
做服务端启动
fserver -type server -cfg fserver.json 

上传文件
fserver -type client -host 127.0.0.1:1789 -path ./cmd/fserver.go

```