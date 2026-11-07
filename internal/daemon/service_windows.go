package daemon

import (
	"context"
	"golang.org/x/sys/windows/svc"
	"log"
)

const serviceName = "AnnoyingWebBlockerDesktop"

type windowsService struct{ options Options }

func RunService(ctx context.Context, options Options) error {
	service, err := svc.IsWindowsService()
	if err != nil {
		return err
	}
	if !service {
		return Run(ctx, options)
	}
	return svc.Run(serviceName, windowsService{options})
}
func (s windowsService) Execute(_ []string, requests <-chan svc.ChangeRequest, changes chan<- svc.Status) (bool, uint32) {
	changes <- svc.Status{State: svc.StartPending}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	done := make(chan error, 1)
	go func() { done <- Run(ctx, s.options) }()
	changes <- svc.Status{State: svc.Running, Accepts: svc.AcceptStop | svc.AcceptShutdown}
	for {
		select {
		case err := <-done:
			if err != nil {
				log.Printf("daemon failed: %v", err)
				return true, 1
			}
			return false, 0
		case request := <-requests:
			switch request.Cmd {
			case svc.Interrogate:
				changes <- request.CurrentStatus
			case svc.Stop, svc.Shutdown:
				changes <- svc.Status{State: svc.StopPending}
				cancel()
				if err := <-done; err != nil {
					return true, 1
				}
				return false, 0
			}
		}
	}
}
