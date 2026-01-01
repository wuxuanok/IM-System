# 思：为什么广播要采用给对应user的channel发消息，让每个user开个go监听channel消息的方式呢？一直监听不是很占用资源吗？为什么广播不直接遍历map然后调用user的sendmsg就完了呢？
答：
这种写法并不是为了节省资源，而是为了
把“网络写”这块最慢、最容易阻塞的环节从广播主流程里彻底解耦；
让慢客户端拖不住整个服务器；
用 Go 的阻塞型 channel 天然做背压（back-pressure），防止内存爆掉。
**直接遍历 map 调 SendMsg —— 看起来省，实则隐患巨大**
conn.Write 最终是系统调用 send()，要把数据拷进内核发送缓冲区。
如果某个客户端接收缓慢、网络抖动、窗口为 0，send() 会阻塞当前 goroutine（除非提前 SetWriteDeadline）。
结果：
– 广播逻辑被拖住，后面所有用户都收不到消息；
– 服务器吞吐量直接掉到最慢那台客户端的速度；
– 万一客户端死机不读，整条广播线程永远卡死。
**每个 user 一个 chan + 专属 goroutine —— 把“慢”隔离出去**
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
**“一直监听”到底占多少资源？**
一个 goroutine 初始栈 2 KB，可动态扩容/缩容，10000 个在线用户 ≈ 20 MB 栈，远小于 10000 个并发线程的代价。
当 channel 空时，range ch 会让 goroutine 挂起休眠，CPU 占用为 0；
一旦连接断开，关闭 channel，goroutine 自动退出，栈被回收。
