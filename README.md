# 思：增加超时强踢功能后，在用户超时被踢后，CPU占有率飙升问题解析
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
