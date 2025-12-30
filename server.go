package main

import (
	"fmt"
	"net"
	"sync"
	"io"
	"time"
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

	user := NewUser(conn, this)

	//用户上线,将用户加入到onlineMap中
	user.Online()

	//监听用户是否活跃的channel
	isLive := make(chan bool)

	//接收客户端发送的消息
	go func() {
		buf := make([]byte, 4096)
		for {
			n, err := conn.Read(buf)
			if n == 0 {
				user.Offline()
				return
			}

			if err != nil && err != io.EOF {
				fmt.Println("Conn Read err:", err)
				return
			}

			//提取用户的消息(去除'\n')
			msg := string(buf[:n-1])

			//用户针对msg进行消息处理(将得到的消息进行广播)
			user.DoMessage(msg)

			//用户的任意消息，代表当前用户是一个活跃的
			isLive <- true
		}
	}()
	

	//当前handler阻塞
	//里面没有任何 case，也就永远等不到可执行的分支
	// 于是当前 goroutine 会 一直挂起，既不会退出，也不会让函数返回。
	for {
		// select里面的case在执行的时候本身就会把case打乱然后进行匹配 
		// 第一次for循环进行select 会随机执行
		// 初始化time.after 然后就等待
		//  isLive要么有数据 要么定时器十秒钟结束 
		// 一旦isLive有数据 本次select结束 进入第二次for循环再次select 初始化time.After 
		// 然后继续等待两个管道是否有数据
		select {
		case <-isLive:
			//当前用户是活跃的，应该重置定时器
			//不做任何事情，为了激活select，更新下面的定时器
		case <-time.After(time.Second * 10):
			//已经超时
			//将当前的User强制关闭
			user.SendMsg("你被踢了")

			//销毁用的资源
			close(user.C)
			conn.Close()

			//退出当前handler
			return //runtime.Goexit()
		}
	}


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