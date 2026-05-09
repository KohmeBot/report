package report

import (
	"fmt"
	"github.com/kohmebot/report/report/daily"
	"github.com/sirupsen/logrus"
	"gorm.io/gorm"
	"time"
)

type BatchRecorder struct {
	db      *gorm.DB
	msgChan chan daily.GroupMessage
}

func NewBatchRecorder(db *gorm.DB) *BatchRecorder {
	return &BatchRecorder{
		db:      db,
		msgChan: make(chan daily.GroupMessage, 500),
	}
}

func (r *BatchRecorder) Write(msg daily.GroupMessage) {
	r.msgChan <- msg
}

// 批量写入，每5秒或积累50条触发一次
func (r *BatchRecorder) batchWriter() {
	ticker := time.NewTicker(5 * time.Second)
	batch := make([]daily.GroupMessage, 0, 50)

	for {
		select {
		case msg := <-r.msgChan:
			batch = append(batch, msg)
			if len(batch) >= 50 { // 积累50条立即写
				batch = r.write(batch)
			}
		case <-ticker.C:
			if len(batch) > 0 { // 定时兜底写入
				batch = r.write(batch)
			}
		}

	}
}

func (r *BatchRecorder) write(batch []daily.GroupMessage) []daily.GroupMessage {
	err := r.db.Create(&batch).Error
	if err != nil {
		logrus.Error(fmt.Errorf("write group message error: %v", err))
	} else {
		logrus.Infof("write %d group message", len(batch))
	}
	return batch[:0]
}
