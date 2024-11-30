package distributedlock

import (
	"context"
	"errors"
	"math/rand"
	"sync/atomic"
	"time"

	"github.com/go-redis/redis/v8"
	"github.com/oklog/ulid/v2"
)

const checkAndDeleteLock = `
  local lockerKey = KEYS[1]
  local targetID = ARGV[1]
  local existedID = redis.call('get',lockerKey)
  if (not existedID or existedID ~= targetID) then
    return 0
  else
		return redis.call('del',lockerKey)
  end
`

const checkAndExtenExpiration = `
  local lockerKey = KEYS[1]
  local targetID = ARGV[1]
  local duration = ARGV[2]
  local existedID = redis.call('get',lockerKey)
  if (not existedID or existedID ~= targetID) then
    return 0
  else
		return redis.call('expire',lockerKey,duration)
  end
`

const (
	minRetryDelayMilliSec = 50
	maxRetryDelayMilliSec = 250
)

// DistributedLock 分布式锁接口
type DistributedLock interface {
	// Lock 加锁
	Lock(key string) error
	// UnLock 解锁
	UnLock(key string)
}

type redisLock struct {
	client          *redis.Client // redis客户端
	expireSec       int           // 锁过期时间
	isBlock         bool          // 是否开启阻塞取锁模式
	id              string        // 身份标识
	watchDogMode    bool
	runningWatchDog int32 // 看门狗是否正在运行
	stopWatchDog    context.CancelFunc
}

func NewRedisLock(client *redis.Client, expireSec int, isBlock, watchDog bool) *redisLock {
	return &redisLock{
		client:          client,
		expireSec:       expireSec,
		isBlock:         isBlock,
		watchDogMode:    watchDog,
		id:              ulid.Make().String(),
		runningWatchDog: 0,
	}
}

func (r *redisLock) Lock(key string) (err error) {
	defer func() {
		if err != nil {
			return
		}
		if !r.watchDogMode {
			return
		}
		// 执行看门狗
		r.runWatchDog(key)
	}()
	isLock, err := r.tryToLock(key)
	if err != nil {
		return err
	}
	if isLock {
		return nil
	}
	// 非阻塞取锁模式下，加锁失败直接返回
	if !r.isBlock {
		return errors.New("lock failed")
	}

	err = r.keepTryingLock(key)
	if err != nil {
		return err
	}

	return nil
}

func (r *redisLock) tryToLock(key string) (bool, error) {
	expiration := time.Duration(r.expireSec) * time.Second
	isSet, err := r.client.SetNX(context.Background(), key, r.id, expiration).Result()
	if err != nil {
		return false, err
	}
	// 加锁成功，返回
	if isSet {
		return true, nil
	}
	return false, nil
}

// keepTryingLock 不断尝试加锁
func (r *redisLock) keepTryingLock(key string) error {
	delayFunc := func() time.Duration {
		return time.Duration(rand.Intn(maxRetryDelayMilliSec-minRetryDelayMilliSec)+minRetryDelayMilliSec) * time.Millisecond
	}
	timeOut := time.After(10 * time.Second)
	timer := time.NewTimer(delayFunc())
	for {
		select {
		case <-timer.C:
			// 尝试加锁一次
			isLock, err := r.tryToLock(key)
			if err != nil {
				return err
			}

			if isLock {
				return nil
			}
			// 加锁失败重置定时器，防止再次加锁冲突
			timer.Reset(delayFunc())
		case <-timeOut:
			// 加锁超时，抛错
			return errors.New("try to lock time out")
		default:
		}
	}
}

// runWatchDog 执行看门狗
func (r *redisLock) runWatchDog(key string) {
	ctx := context.Background()
	newCtx, cancelFunc := context.WithCancel(ctx)
	r.stopWatchDog = cancelFunc
	// 在执行之前，确保看门狗未在执行
	for atomic.CompareAndSwapInt32(&r.runningWatchDog, 0, 1) {
	}
	go func() {
		defer func() {
			atomic.StoreInt32(&r.runningWatchDog, 0)
		}()

		ticker := time.NewTicker(time.Duration(10) * time.Second)
		defer ticker.Stop()

		for range ticker.C {
			select {
			case <-newCtx.Done():
				return
			default:
				r.extendExpiration(newCtx, key)
			}
		}
	}()
}

// extendExpiration 更新锁的过期时间
func (r *redisLock) extendExpiration(ctx context.Context, key string) {
	keys := []string{key}
	args := []interface{}{r.id, r.expireSec + 2}
	_, err := r.client.Eval(ctx, checkAndExtenExpiration, keys, args...).Result()
	if err != nil {
		return
	}
}

// UnLock 解锁
func (r *redisLock) UnLock(key string) {
	defer func() {
		if r.stopWatchDog != nil {
			r.stopWatchDog()
		}
	}()
	ctx := context.Background()
	keys := []string{key}
	args := []interface{}{r.id}
	_, err := r.client.Eval(ctx, checkAndDeleteLock, keys, args...).Result()
	if err != nil {
		return
	}
}
