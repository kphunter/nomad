package taskrunner

import (
	"context"

	hclog "github.com/hashicorp/go-hclog"
	"github.com/hashicorp/nomad/client/allocrunner/interfaces"
	"github.com/hashicorp/nomad/nomad/structs"
	"github.com/hashicorp/nomad/plugins/drivers"
)

var _ interfaces.TaskPrestartHook = (*remoteTaskHook)(nil)
var _ interfaces.TaskPreKillHook = (*remoteTaskHook)(nil)

// remoteTaskHook reattaches to remotely executing tasks.
type remoteTaskHook struct {
	tr *TaskRunner

	logger hclog.Logger
}

func newRemoteTaskHook(tr *TaskRunner, logger hclog.Logger) interfaces.TaskHook {
	h := &remoteTaskHook{
		tr: tr,
	}
	h.logger = logger.Named(h.Name())
	return h
}

func (h *remoteTaskHook) Name() string {
	return "remote_task"
}

// Prestart performs 2 remote task driver related tasks:
//   1. If there is no local handle, see if there is a handle propagated from a
//      previous alloc to be restored.
//   2. If the alloc is lost make sure the task signal is set to detach instead
//      of kill.
func (h *remoteTaskHook) Prestart(ctx context.Context, req *interfaces.TaskPrestartRequest, resp *interfaces.TaskPrestartResponse) error {
	// Ensure the signal is set according to the allocation's state
	h.setSignal(h.tr.Alloc())

	if h.tr.getDriverHandle() != nil {
		// Driver handle already exists so don't try to load remote
		// task handle
		return nil
	}

	//TODO(schmichael) create better abstraction for retrieving task handle
	h.tr.stateLock.Lock()
	th := drivers.NewTaskHandleFromState(h.tr.state)
	h.tr.stateLock.Unlock()

	// Task handle will be nil if there was no previous allocation or if
	// this is a destructive update
	if th == nil {
		resp.Done = true
		return nil
	}

	// The task config is unique per invocation so recreate it here
	th.Config = h.tr.buildTaskConfig()

	if err := h.tr.driver.RecoverTask(th); err != nil {
		//FIXME(schmichael) soft error here to let a new instance get
		//started?
		h.logger.Error("error recovering task state", "error", err)
		return nil
	}

	taskInfo, err := h.tr.driver.InspectTask(th.Config.ID)
	if err != nil {
		//FIXME(schmichael) soft error here to let a new instance get
		//started?
		h.logger.Error("error inspecting recovered task state", "error", err)
		return nil
	}

	h.logger.Debug("=====> using remote task handle")

	h.tr.setDriverHandle(NewDriverHandle(h.tr.driver, th.Config.ID, h.tr.Task(), taskInfo.NetworkOverride))

	h.tr.stateLock.Lock()
	h.tr.localState.TaskHandle = th
	h.tr.localState.DriverNetwork = taskInfo.NetworkOverride
	h.tr.stateLock.Unlock()

	h.tr.UpdateState(structs.TaskStateRunning, structs.NewTaskEvent(structs.TaskStarted))

	return nil
}

func (h *remoteTaskHook) Update(ctx context.Context, _ *interfaces.TaskUpdateRequest, _ *interfaces.TaskUpdateResponse) error {
	alloc := h.tr.Alloc()
	h.logger.Debug("===> Updating Remote Task Hook", "desired", alloc.DesiredStatus, "client", alloc.ClientStatus)
	h.setSignal(alloc)
	return nil
}

// PreKilling tells the remote task driver to detach a remote task instead of
// stopping it.
//
//FIXME(schmichael) this is a hacky way to signal "detach" instead of "destroy"
//and requires the driver handle to keep extra state
func (h *remoteTaskHook) PreKilling(ctx context.Context, req *interfaces.TaskPreKillRequest, resp *interfaces.TaskPreKillResponse) error {
	alloc := h.tr.Alloc()
	h.logger.Debug("===> PreKilling Remote Task Hook", "desired", alloc.DesiredStatus, "client", alloc.ClientStatus)
	h.setSignal(alloc)
	return nil
}

// setSignal to detach if the allocation is lost or draining. Safe to call
// multiple times as it only transitions to using detach -- never back to kill.
func (h *remoteTaskHook) setSignal(alloc *structs.Allocation) {
	switch {
	case alloc.ClientStatus == structs.AllocClientStatusLost:
		// Continue on; lost allocs should just detach
	case alloc.DesiredTransition.ShouldMigrate():
		// Continue on; migrating allocs should just detach
	default:
		// Nothing to do exit early
		return
	}

	driverHandle := h.tr.getDriverHandle()
	if driverHandle == nil {
		h.logger.Debug("===> PreKilling Remote Task Hook NO DRIVER HANDLE", "desired", alloc.DesiredStatus, "client", alloc.ClientStatus)
		// Nothing to do exit early
		return
	}

	//HACK DetachSignal indicates to the remote task driver that it should
	//detach this remote task and ignore further actions against it.
	h.logger.Debug("===> PRE  SetKillSignal")
	driverHandle.SetKillSignal(drivers.DetachSignal)
	h.logger.Debug("===> POST SetKillSignal")
}
