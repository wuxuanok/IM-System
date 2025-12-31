package main

import (
	"fmt"
	"io"
	"net"
	"sync"
	"time"
)

type Server struct {
	Ip   string
	Port int

	//在线用户的列表
	OnlineMap map[string]*User
	mapLock   sync.RWMutex

	//消息广播的channel
	Message chan string
}

// 创建一个Server的接口
func NewServer(ip string, port int) *Server {
	server := &Server{
		Ip:        ip,
		Port:      port,
		OnlineMap: make(map[string]*User),
		Message:   make(chan string),
	}
	return server
}

// 广播消息的方法
func (this *Server) BroadCast(user *User, msg string) {
	sendMsg := "[" + user.Addr + "]" + user.Name + ":" + msg

	this.Message <- sendMsg
}

// 监听广播消息channel的goroutine，一旦有消息就发送给全部的在线User
func (this *Server) ListenMessage() {
	for {
		msg := <-this.Message

		//将msg发送给全部的在线User
		this.mapLock.Lock()
		for _, cli := range this.OnlineMap {
			cli.C <- msg
		}
		this.mapLock.Unlock()
	}
}

// 这段代码在哪一端跑？	服务器端
// 客户端（nc 命令） ←→ TCP 三次握手 ←→ 服务器（Go 程序）
// conn.Read() 读的是谁？	客户端发来的数据
// conn.Write() 写给谁？	客户端
// conn.Close() 谁先挂电话？	服务器先挂，对方（客户端）会收到 RST 或 EOF
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
			//EOF 属于 err == io.EOF 且 n == 0
			if n == 0 {
				user.Offline()
				return
			}

			if err != nil && err != io.EOF {
				//如果是 RST 或其他错误，err != io.EOF 成立，走进第二个 if，也 return 结束
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

		//所有 case 都 ready → 运行时随机挑一个执行；
		//一旦某个 case 被执行，整个 select 就结束了。不需要、也不能写 break
		//不会“继续执行下面的 case”；每个 case 是它自己的独立分支，执行完就退出 select
		select {
		case <-isLive:
			//只要用户发消息，读 conn 的 goroutine 就会往 isLive 里丢一个值。
			// 这个 case 被触发 → 什么业务都不做，但整个 select 会立即结束并进入下一次 for 循环。
			// 结束 = 丢弃旧的 time.After 定时器，下一次循环会重新新建一个 10 秒定时器，相当于“续命”。
		case <-time.After(time.Second * 300):
			//作用：立即返回一个只读通道（<-chan time.Time），这个通道会在指定时长后收到一个 time.Time 值。
			// 在 select 里作为 case 条件，表示“等待这个通道可读”。
			// 因此 <-time.After(10 * time.Second) 就是“先等 10 秒，时间到了才能继续往下走”。

			//将当前的User强制关闭
			user.SendMsg("你被踢了")

			//销毁用的资源

			// 通道立即变为“已关闭”状态；后续任何 <-user.C 不再阻塞，而是返回零值 + ok=false。
			// ListenMessage 里的 for msg := range user.C 会在缓存读完后自动跳出循环，goroutine 自然结束。
			close(user.C)
			//EOF = 正常结束，可以收拾桌子离场；
			// RST = 异常掉线，需要记录日志、清理资源。
			/*
				网络连接 conn.Close()
				TCP 发送 RST，对端 Read 返回 io.EOF（或 err != nil）。
				读 goroutine：Read 得 EOF/err → 执行 Offline() → return 结束
				写 goroutine：for改为range this.C后， 在通道关闭且缓存空后自动退出，不再 Write，故不会空转 CPU
			*/
			conn.Close()

			//退出当前handler
			return //runtime.Goexit()
		}
	}

}

// 启动服务器的接口
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
