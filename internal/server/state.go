package server

import (
	"sync"
	"time"

	"salad-bzminer-worker/internal/bzapi"
	"salad-bzminer-worker/internal/gpu"
)

type Phase string

const (
	PhaseStarting Phase = "starting"
	PhaseRunning  Phase = "running"
	PhaseBackoff  Phase = "backoff"
	PhaseStopping Phase = "stopping"
	PhaseFatal    Phase = "fatal"
)

type ExitInfo struct {
	Code   int
	Signal string
	At     time.Time
}

type State struct {
	mu sync.RWMutex

	phase          Phase
	startedAt      time.Time
	gpus           []gpu.GPU
	gpuSource      string
	workerName     string
	minerPID       int
	minerRunning   bool
	minerStartedAt time.Time
	minerExit      *ExitInfo
	restarts       int

	apiSupported   bool
	snapshot       *bzapi.Snapshot
	lastAPISuccess time.Time
	lastAPIError   string
}

type View struct {
	Phase          Phase
	StartedAt      time.Time
	GPUs           []gpu.GPU
	GPUSource      string
	WorkerName     string
	MinerPID       int
	MinerRunning   bool
	MinerStartedAt time.Time
	MinerExit      *ExitInfo
	Restarts       int
	APISupported   bool
	Snapshot       *bzapi.Snapshot
	LastAPISuccess time.Time
	LastAPIError   string
}

func NewState() *State {
	return &State{phase: PhaseStarting, startedAt: time.Now()}
}

func (s *State) SetPhase(p Phase) {
	s.mu.Lock()
	s.phase = p
	s.mu.Unlock()
}

func (s *State) SetGPUs(gpus []gpu.GPU, source string) {
	s.mu.Lock()
	s.gpus = append([]gpu.GPU(nil), gpus...)
	s.gpuSource = source
	s.mu.Unlock()
}

func (s *State) SetWorker(name string) {
	s.mu.Lock()
	s.workerName = name
	s.mu.Unlock()
}

func (s *State) SetAPISupported(v bool) {
	s.mu.Lock()
	s.apiSupported = v
	s.mu.Unlock()
}

func (s *State) SetMinerRunning(pid int) {
	s.mu.Lock()
	s.minerPID = pid
	s.minerRunning = true
	s.minerStartedAt = time.Now()
	s.phase = PhaseRunning
	s.mu.Unlock()
}

func (s *State) SetMinerStopped(info ExitInfo) {
	s.mu.Lock()
	s.minerRunning = false
	s.minerExit = &info
	s.mu.Unlock()
}

func (s *State) SetAPISuccess(snap *bzapi.Snapshot) {
	s.mu.Lock()
	s.snapshot = snap
	s.lastAPISuccess = snap.FetchedAt
	s.lastAPIError = ""
	s.mu.Unlock()
}

func (s *State) SetAPIError(err error) {
	s.mu.Lock()
	if err == nil {
		s.lastAPIError = ""
	} else {
		s.lastAPIError = err.Error()
	}
	s.mu.Unlock()
}

func (s *State) IncRestarts() {
	s.mu.Lock()
	s.restarts++
	s.mu.Unlock()
}

func (s *State) View() View {
	s.mu.RLock()
	defer s.mu.RUnlock()
	var snap *bzapi.Snapshot
	if s.snapshot != nil {
		cp := *s.snapshot
		snap = &cp
	}
	var exit *ExitInfo
	if s.minerExit != nil {
		cp := *s.minerExit
		exit = &cp
	}
	return View{
		Phase:          s.phase,
		StartedAt:      s.startedAt,
		GPUs:           append([]gpu.GPU(nil), s.gpus...),
		GPUSource:      s.gpuSource,
		WorkerName:     s.workerName,
		MinerPID:       s.minerPID,
		MinerRunning:   s.minerRunning,
		MinerStartedAt: s.minerStartedAt,
		MinerExit:      exit,
		Restarts:       s.restarts,
		APISupported:   s.apiSupported,
		Snapshot:       snap,
		LastAPISuccess: s.lastAPISuccess,
		LastAPIError:   s.lastAPIError,
	}
}

// ReadyReasons returns the ordered reasons a worker is not ready. Liveness is
// separate: a dead miner with a working health server is never ready.
func (v View) ReadyReasons(now time.Time, startupTimeout time.Duration) []string {
	var reasons []string
	if len(v.GPUs) == 0 {
		reasons = append(reasons, "gpu_not_detected")
	}
	if !v.MinerRunning {
		reasons = append(reasons, "miner_not_running")
	}
	if !v.APISupported {
		if v.MinerRunning && now.Sub(v.MinerStartedAt) < 30*time.Second {
			reasons = append(reasons, "miner_starting")
		}
		return reasons
	}
	if v.LastAPISuccess.IsZero() {
		if !v.MinerRunning {
			return reasons
		}
		if now.Sub(v.MinerStartedAt) > startupTimeout {
			reasons = append(reasons, "startup_timeout")
		} else {
			reasons = append(reasons, "miner_starting")
		}
		return reasons
	}
	if now.Sub(v.LastAPISuccess) > 30*time.Second {
		reasons = append(reasons, "miner_api_unreachable")
	}
	return reasons
}
