package distributedlock

import (
	"context"
	"fmt"
	"log"
	"strconv"
	"sync"

	"github.com/go-redis/redis/v8"
)

var (
	rOnce sync.Once
	r     *redis.Client
)

func NewRedis(ip, password string, port int) *redis.Client {
	rOnce.Do(func() {
		ctx := context.Background()
		addr := ip + ":" + strconv.Itoa(port)
		// 创建 Redis 客户端
		r = redis.NewClient(&redis.Options{
			Addr:     addr,     // Redis 服务器地址
			Password: password, // 没有密码时留空
			DB:       0,        // 默认 DB
		})

		// 测试连接
		err := r.Ping(ctx).Err()
		if err != nil {
			log.Fatalf("Unable to connect to Redis: %v", err)
		}
		fmt.Println("Connect to Redis successfully!")

	})
	return r
}
