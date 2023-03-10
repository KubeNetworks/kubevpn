package controlplane

import (
	"log"
	"time"

	"github.com/fsnotify/fsnotify"
)

type OperationType int

const (
	Create OperationType = iota
	Remove
	Modify
)

type NotifyMessage struct {
	Operation OperationType
	FilePath  string
}

func Watch(watcher *fsnotify.Watcher, filename string, notifyCh chan<- NotifyMessage) {
	for {
		select {
		case event, ok := <-watcher.Events:
			if !ok {
				return
			}
			if event.Op&fsnotify.Write == fsnotify.Write {
				notifyCh <- NotifyMessage{
					Operation: Modify,
					FilePath:  event.Name,
				}
			} else if event.Op&fsnotify.Create == fsnotify.Create {
				notifyCh <- NotifyMessage{
					Operation: Create,
					FilePath:  event.Name,
				}
			} else if event.Op&fsnotify.Remove == fsnotify.Remove {
				notifyCh <- NotifyMessage{
					Operation: Remove,
					FilePath:  event.Name,
				}
			}

		case err, ok := <-watcher.Errors:
			if !ok {
				return
			}
			log.Println("error:", err)

		case <-time.Tick(time.Second * 3):
			notifyCh <- NotifyMessage{
				Operation: Modify,
				FilePath:  filename,
			}
		}
	}
}
