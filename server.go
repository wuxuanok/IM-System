package main

import (
	"fmt"
	"net"
	"sync"
	"io"
)

type Server struct {
	Ip string
	Port int

	//在线用户的列表
	OnlineMap map[string]*User
	mapLock sync.RWMutex

	//消息广播的channel
	Message chan string
}

//创建一个Server的接口
func NewServer (ip string, port int) *Server {
	server := &Server{
		Ip:	ip,
		Port: port,
		OnlineMap: make(map[string]*User),
		Message: make(chan string),
	}
	return server
}

//广播消息的方法
func (this *Server) BroadCast(user *User, msg string) {
	sendMsg := "[" + user.Addr + "]" + user.Name + ":" + msg
	
	this.Message <- sendMsg
}

//监听广播消息channel的goroutine，一旦有消息就发送给全部的在线User
func (this *Server) ListenMessage() {
	for {
		msg := <- this.Message

		//将msg发送给全部的在线User
		this.mapLock.Lock()
		for _, cli := range this.OnlineMap {
			cli.C <- msg
		}
		this.mapLock.Unlock()
	}
}

func (this *Server) Handler(conn net.Conn) {
	//...当前链接的业务
	// fmt.Println("链接建立成功")

	user := NewUser(conn)

	//用户上线,将用户加入到onlineMap中
	this.mapLock.Lock()
	this.OnlineMap[user.Name] = user
	this.mapLock.Unlock()
	
	//广播当前用户上线消息
	this.BroadCast(user, "已上线")

	//接收客户端发送的消息
	go func() {
		buf := make([]byte, 4096)
		for {
			n, err := conn.Read(buf)
			if n == 0 {
				this.BroadCast(user, "下线")
				return
			}

			if err != nil && err != io.EOF {
				fmt.Println("Conn Read err:", err)
				return
			}

			//提取用户的消息(去除'\n')
			msg := string(buf[:n-1])

			//将得到的消息进行广播
			this.BroadCast(user, msg)
		}
	}()
	

	//当前handler阻塞
	//里面没有任何 case，也就永远等不到可执行的分支
	// 于是当前 goroutine 会 一直挂起，既不会退出，也不会让函数返回。
	select {}

}

//启动服务器的接口
func (this *Server) Start() {
	//socket listen
	listener, err := net.Listen("tcp", fmt.Sprintf("%s:%d", this.Ip, this.Port))
	if err != nil {
		fmt.Println("net.Listen err:", err)
		return 
	}
	//close listen socket
	defer listener.Close()

	//启动监听Message的goroutine
	go this.ListenMessage()

	for {
		// accept
		conn, err := listener.Accept()
		if err != nil {
			fmt.Println("listen accept err:", err)
			continue
		}
		//do handler
		go this.Handler(conn)
	}


}