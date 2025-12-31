package main

import (
	"flag"
	"fmt"
	"net"
)

type Client struct {
	ServerIp   string
	ServerPort int
	Name       string
	conn       net.Conn
}

func NewClient(serverIp string, serverPort int) *Client {
	//创建客户端对象
	client := &Client{
		ServerIp:   serverIp,
		ServerPort: serverPort,
	}
	//连接server
	conn, err := net.Dial("tcp", fmt.Sprintf("%s:%d", serverIp, serverPort))
	if err != nil {
		fmt.Println("net.Dial error:", err)
		return nil
	}
	client.conn = conn

	//返回对象
	return client
}

var severIp string
var serverPort int

// ./client -ip 127.0.0.1

func init() {
	flag.StringVar(&severIp, "ip", "127.0.0.1", "设置服务器IP地址(默认是127.0.0.1)")
	flag.IntVar(&serverPort, "port", 8090, "设置服务器端口(默认是8090)")
}

func main() {
	//命令行解析
	flag.Parse()

	// client := NewClient("127.0.0.1", 8090)
	client := NewClient(severIp, serverPort)
	if client == nil {
		fmt.Println(">>>>>>连接服务器失败>>>>>>")
		return
	}
	fmt.Println(">>>>>>连接服务器成功>>>>>>")

	//启动客户端的业务
	select {}
}
