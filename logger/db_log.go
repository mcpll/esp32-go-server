package logger

import (
  "fmt"
  log "github.com/sirupsen/logrus"
)

type GormLog struct {
  clog *log.Logger
}

var DbLog *GormLog

func InitDbLog(clog *log.Logger) {
  DbLog = &GormLog{
    clog: clog,
  }
}

func (d *GormLog) Printf(format string, args ...interface{}) {
  logStr := fmt.Sprintf(format, args...)
  d.clog.Info(logStr)
}
