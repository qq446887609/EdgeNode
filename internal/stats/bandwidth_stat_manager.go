// Copyright 2022 Liuxiangchao iwind.liu@gmail.com. All rights reserved. Official site: https://goedge.cn .

package stats

import (
	"github.com/TeaOSLab/EdgeCommon/pkg/rpc/pb"
	"github.com/TeaOSLab/EdgeNode/internal/events"
	"github.com/TeaOSLab/EdgeNode/internal/goman"
	"github.com/TeaOSLab/EdgeNode/internal/remotelogs"
	"github.com/TeaOSLab/EdgeNode/internal/rpc"
	"github.com/iwind/TeaGo/logs"
	"github.com/iwind/TeaGo/types"
	timeutil "github.com/iwind/TeaGo/utils/time"
	"sync"
	"time"
)

var SharedBandwidthStatManager = NewBandwidthStatManager()

func init() {
	events.On(events.EventLoaded, func() {
		goman.New(func() {
			SharedBandwidthStatManager.Start()
		})
	})
}

type BandwidthStat struct {
	Day      string
	TimeAt   string
	UserId   int64
	ServerId int64

	CurrentBytes     int64
	CurrentTimestamp int64
	MaxBytes         int64
}

// BandwidthStatManager 服务带宽统计
type BandwidthStatManager struct {
	m map[string]*BandwidthStat // key => *BandwidthStat

	lastTime string // 上一次执行的时间

	ticker *time.Ticker
	locker sync.Mutex
}

func NewBandwidthStatManager() *BandwidthStatManager {
	return &BandwidthStatManager{
		m:      map[string]*BandwidthStat{},
		ticker: time.NewTicker(1 * time.Minute), // 时间小于1分钟是为了更快速地上传结果
	}
}

func (this *BandwidthStatManager) Start() {
	for range this.ticker.C {
		err := this.Loop()
		if err != nil && !rpc.IsConnError(err) {
			remotelogs.Error("BANDWIDTH_STAT_MANAGER", err.Error())
		}
	}
}

func (this *BandwidthStatManager) Loop() error {
	var now = time.Now()
	var day = timeutil.Format("Ymd", now)
	var currentTime = timeutil.FormatTime("Hi", now.Unix()/300*300)

	if this.lastTime == currentTime {
		return nil
	}
	this.lastTime = currentTime

	var pbStats = []*pb.ServerBandwidthStat{}

	this.locker.Lock()
	for key, stat := range this.m {
		if stat.Day < day || stat.TimeAt < currentTime {
			pbStats = append(pbStats, &pb.ServerBandwidthStat{
				Id:       0,
				UserId:   stat.UserId,
				ServerId: stat.ServerId,
				Day:      stat.Day,
				TimeAt:   stat.TimeAt,
				Bytes:    stat.MaxBytes,
			})
			delete(this.m, key)
		}
	}
	this.locker.Unlock()

	if len(pbStats) > 0 {
		// 上传
		rpcClient, err := rpc.SharedRPC()
		if err != nil {
			return err
		}
		_, err = rpcClient.ServerBandwidthStatRPC().UploadServerBandwidthStats(rpcClient.Context(), &pb.UploadServerBandwidthStatsRequest{ServerBandwidthStats: pbStats})
		if err != nil {
			return err
		}
	}

	return nil
}

// Add 添加带宽数据
func (this *BandwidthStatManager) Add(userId int64, serverId int64, bytes int64) {
	if serverId <= 0 || bytes == 0 {
		return
	}

	var now = time.Now()
	var timestamp = now.Unix()
	var day = timeutil.Format("Ymd", now)
	var timeAt = timeutil.FormatTime("Hi", now.Unix()/300*300)
	var key = types.String(serverId) + "@" + day + "@" + timeAt

	this.locker.Lock()
	stat, ok := this.m[key]
	if ok {
		// 此刻如果发生用户ID（userId）的变化也忽略，等N分钟后有新记录后再换

		if stat.CurrentTimestamp == timestamp {
			stat.CurrentBytes += bytes
		} else {
			stat.CurrentBytes = bytes
			stat.CurrentTimestamp = timestamp
		}
		if stat.CurrentBytes > stat.MaxBytes {
			stat.MaxBytes = stat.CurrentBytes
		}
	} else {
		this.m[key] = &BandwidthStat{
			Day:              day,
			TimeAt:           timeAt,
			UserId:           userId,
			ServerId:         serverId,
			CurrentBytes:     bytes,
			MaxBytes:         bytes,
			CurrentTimestamp: timestamp,
		}
	}
	this.locker.Unlock()
}

func (this *BandwidthStatManager) Inspect() {
	this.locker.Lock()
	logs.PrintAsJSON(this.m)
	this.locker.Unlock()
}
