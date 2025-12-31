package main

import (
	"net"
	"strings"
)

type User struct {
	Name   string
	Addr   string
	C      chan string
	conn   net.Conn
	server *Server
}

// 创建一个用户的API
func NewUser(conn net.Conn, server *Server) *User {
	userAddr := conn.RemoteAddr().String()

	user := &User{
		Name:   userAddr,
		Addr:   userAddr,
		C:      make(chan string),
		conn:   conn,
		server: server,
	}

	//启动监听当前user channel消息的goroutine
	go user.ListenMessage()
	return user
}

// 用户的上线业务
func (this *User) Online() {

	//用户上线,将用户加入到onlineMap中
	this.server.mapLock.Lock()
	this.server.OnlineMap[this.Name] = this
	this.server.mapLock.Unlock()

	//广播当前用户上线消息
	this.server.BroadCast(this, "已上线")
}

// 用户的下线业务
func (this *User) Offline() {
	//用户下线,将用户从onlineMap中删除
	this.server.mapLock.Lock()
	delete(this.server.OnlineMap, this.Name)
	this.server.mapLock.Unlock()

	//广播当前用户下线消息
	this.server.BroadCast(this, "下线")
}

// 给当前User对应的客户端发送消息
func (this *User) SendMsg(msg string) {
	this.conn.Write([]byte(msg))
}

// 用户处理消息的业务
func (this *User) DoMessage(msg string) {
	if msg == "who" {
		//查询当前在线用户都有哪些
		this.server.mapLock.Lock()
		for _, user := range this.server.OnlineMap {
			onlineMsg := "[" + user.Addr + "]" + user.Name + ":" + "在线...\n"
			this.SendMsg(onlineMsg)
		}
		this.server.mapLock.Unlock()

	} else if len(msg) > 7 && msg[:7] == "rename|" {
		//消息格式: rename|张三
		newName := strings.Split(msg, "|")[1]

		//判断name是否存在
		_, ok := this.server.OnlineMap[newName]

		if ok {
			this.SendMsg("当前用户名被使用\n")
		} else {
			this.server.mapLock.Lock()
			delete(this.server.OnlineMap, this.Name)
			this.server.OnlineMap[newName] = this
			this.server.mapLock.Unlock()

			this.Name = newName
			this.SendMsg("您已经更新用户名:" + this.Name + "\n")
		}

	} else if len(msg) > 4 && msg[:3] == "to|" {
		//消息格式: to|张三|消息内容

		//获取对方的用户名
		remoteName := strings.Split(msg, "|")[1]
		if remoteName == "" {
			this.SendMsg("消息格式不正确，请使用 \"to|张三|你好啊\"格式。\n")
			return
		}
		//根据用户名 得到对方User对象
		remoteUser, ok := this.server.OnlineMap[remoteName]
		if !ok {
			this.SendMsg("该用户名不存在\n")
			return
		}

		//获取消息内容，通过对方的User对象将消息内容发送过去
		content := strings.Split(msg, "|")[2]
		if content == "" {
			this.SendMsg("无消息内容，请重发\n")
			return
		}
		remoteUser.SendMsg(this.Name + "对您说:" + content + "\n")

	} else {
		//将得到的消息进行广播
		this.server.BroadCast(this, msg)
	}

}

// 监听当前User channel的 方法，一旦有消息，直接发送给对应用户的电脑
func (this *User) ListenMessage() {
	//此处本来是单独的for死循环，然而超时强踢关掉C通道后，出现了CPU飙升问题，原因如下：
	//<- 对已关闭通道的读操作永远不阻塞，行为分两种：
	// 通道里还有残留数据 : 先把缓存数据逐个返回；此时 msg, ok := <-ch 的 ok == true，值就是元素本身。
	// 缓存已空 : 立即返回零值（"" 对于 chan string），且 ok == false；
	// 但 裸读 msg := <-ch 不会 panic，也不会阻塞，只是拿到的是零值，看起来“仍然成功执行”。
	//在关闭后确实会继续执行，只是 msg 恒等于 ""，造成for 循环变成无限空转，CPU 就被吃光了。

	/* 	for {
		msg := <-this.C

		this.conn.Write([]byte(msg + "\n"))
	} */

	//解决方法:根本思路：用能识别关闭状态的语法
	// Go 提供了两种现成办法，任选其一即可彻底避免“空缓存后拿到零值死循环”。

	//range 迭代——最简洁	通道关闭 且缓存已空 时自动退出循环，无需任何判断。
	for msg := range this.C {
		//当服务器 已 conn.Close() 后再走到 Write，内核会返回 use of closed network connection；
		this.conn.Write([]byte(msg + "\n"))
	}
	// 出循环到这里说明通道已关闭，goroutine 可正常结束

	//逗号 ok 惯用法——显式判断	读到零值时看 ok 标志，为 false 就 break。

	/*     for {
	        msg, ok := <-this.C
	        if !ok {
				// 通道关闭且缓存已空，则跳出循环
	            break
	        }
	        this.conn.Write([]byte(msg + "\n"))
	    } */

}
