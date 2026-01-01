# 思：增加超时强踢功能后，在用户超时被踢后，CPU占有率飙升问题解析
## 超时强踢代码
```go
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
```
## 原来的用户监听消息方法
```go
func (this *User) ListenMessage() {
	for {
		msg := <-this.C
		this.conn.Write([]byte(msg + "\n"))
	}
}
```

	<- 对已关闭通道的读操作永远不阻塞，行为分两种：
	通道里还有残留数据 : 先把缓存数据逐个返回；此时 msg, ok := <-ch 的 ok == true，值就是元素本身。
	缓存已空 : 立即返回零值（"" 对于 chan string），且 ok == false；
	但 裸读 msg := <-ch 不会 panic，也不会阻塞，只是拿到的是零值，看起来“仍然成功执行”。
	在关闭后确实会继续执行，只是 msg 恒等于 ""，造成for 循环变成无限空转，CPU 就被吃光了。
## 解决方法
**根本思路：用能识别关闭状态的语法**
Go 提供了两种现成办法，任选其一即可彻底避免“空缓存后拿到零值死循环”。
### 方法一:range 迭代——最简洁	通道关闭 且缓存已空 时自动退出循环，无需任何判断。
```go
	for msg := range this.C {
		//当服务器 已 conn.Close() 后再走到 Write，内核会返回 use of closed network connection；
		this.conn.Write([]byte(msg + "\n"))
	}
	// 出循环到这里说明通道已关闭，goroutine 可正常结束
```
### 方法二:逗号 ok 惯用法——显式判断	读到零值时看 ok 标志，为 false 就 break。
```go
    for {
	        msg, ok := <-this.C
	        if !ok {
				// 通道关闭且缓存已空，则跳出循环
	            break
	        }
	        this.conn.Write([]byte(msg + "\n"))
	    }
```

# 思：为什么广播要采用给对应user的channel发消息，让每个user开个go监听channel消息的方式呢？一直监听不是很占用资源吗？为什么广播不直接遍历map然后调用user的sendmsg就完了呢？
答：
## 这种写法的意义
这种写法并不是为了节省资源，而是为了
把“网络写”这块最慢、最容易阻塞的环节从广播主流程里彻底解耦；
让慢客户端拖不住整个服务器；
用 Go 的阻塞型 channel 天然做背压（back-pressure），防止内存爆掉。
## **直接遍历 map 调 SendMsg —— 看起来省，实则隐患巨大**
conn.Write 最终是系统调用 send()，要把数据拷进内核发送缓冲区。
如果某个客户端接收缓慢、网络抖动、窗口为 0，send() 会阻塞当前 goroutine（除非提前 SetWriteDeadline）。
结果：
– 广播逻辑被拖住，后面所有用户都收不到消息；
– 服务器吞吐量直接掉到最慢那台客户端的速度；
– 万一客户端死机不读，整条广播线程永远卡死。
## **每个 user 一个 chan + 专属 goroutine —— 把“慢”隔离出去**
```
广播主流程
   ↓（只是把字符串塞进 chan，O(1) 内存拷贝）
this.Message <- msg

ListenMessage() // 每个 user 一个 goroutine
   ↓
for msg := range user.C {
    conn.Write(msg)   // 即使这里阻塞，也只阻塞自己
}
```
广播线程只做非阻塞写 channel（channel 带缓冲时几乎瞬间完成）。
谁慢谁阻塞自己的 goroutine，不影响别人，也不影响服务器继续接收新消息。
当某个客户端迟迟不读，它的 channel 会堆满，写端（ListenMessage）阻塞，天然产生背压：
– 不会无限占用服务器内存；
– 更不会因为一个僵尸连接把整台服务器拖死。
## **“一直监听”到底占多少资源？**
一个 goroutine 初始栈 2 KB，可动态扩容/缩容，10000 个在线用户 ≈ 20 MB 栈，远小于 10000 个并发线程的代价。
当 channel 空时，range ch 会让 goroutine 挂起休眠，CPU 占用为 0；
一旦连接断开，关闭 channel，goroutine 自动退出，栈被回收。
