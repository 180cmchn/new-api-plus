package service

import (
	"sync"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
	"github.com/bytedance/gopkg/util/gopool"
)

var commissionTaskOnce sync.Once

func StartCommissionSettlementTask() {
	commissionTaskOnce.Do(func() {
		if !common.IsMasterNode {
			return
		}
		gopool.Go(func() {
			// Retry due work each minute; the durable day boundary is Beijing 00:00.
			// Running immediately also catches up after a restart or outage.
			for {
				for {
					n, err := model.SettleDailyCommissions(time.Now().Unix(), 100)
					if err != nil {
						common.SysError(common.LogText("daily commission settlement failed: %s", err.Error()))
						break
					}
					if n == 0 {
						break
					}
				}
				now := time.Now()
				next := now.Truncate(time.Minute).Add(time.Minute)
				time.Sleep(time.Until(next))
			}
		})
	})
}
