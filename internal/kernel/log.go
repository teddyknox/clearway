package kernel

import "github.com/teddyknox/clearway/internal/traj"

// InputKind names one kind of kernel input.
type InputKind uint8

const (
	InAdvance InputKind = iota
	InAdmit
	InPropose
	InRecontract
	InAck
	InRefuse
	InFail
	InTrack
	InMarkIntruder
	InReanchor
	InRemove
	InTick
	InSetAuthority
	NumInputKinds
)

// HasVerdict reports whether the kernel answers an input of this kind with a
// verdict.
func (kd InputKind) HasVerdict() bool {
	return kd == InAdmit || kd == InPropose || kd == InRecontract
}

// Input is one input of the kernel. The kernel is a deterministic state
// machine: the same inputs in the same order produce the same state and the
// same verdicts.
//
// Each kind uses some of the fields:
//
//	InAdvance                      T
//	InAdmit                        Flight, Traj
//	InPropose, InRecontract        Flight, Traj, By
//	InTrack                        Flight, Track
//	InSetAuthority                 Flight, By
//	InAck, InRefuse, InFail,
//	InMarkIntruder, InRemove       Flight
//	InReanchor, InTick             none
type Input struct {
	Kind   InputKind
	T      int64 // µs
	Flight FlightID
	Traj   *traj.Trajectory
	Track  Track
	By     Authority
}

// Recorder receives the inputs and the verdicts of one kernel, in the order
// in which the kernel handles them.
//
// The kernel calls Input before it applies the input. If the kind of the
// input has a verdict, the kernel then calls Verdict once, before the
// operation returns. The kernel makes no other calls.
type Recorder interface {
	Input(in Input)
	Verdict(v Verdict)
}

func (k *Kernel) record(in Input) {
	if k.Rec != nil {
		k.Rec.Input(in)
	}
}

// Apply applies one input, as the operation of its kind does. has is true if
// the kind has a verdict, and v is then that verdict.
//
// The caller must pass a defined kind. Apply ignores any other.
func (k *Kernel) Apply(in Input) (v Verdict, has bool) {
	switch in.Kind {
	case InAdvance:
		k.Advance(in.T)
	case InAdmit:
		return k.Admit(in.Flight, in.Traj), true
	case InPropose:
		return k.Propose(in.Flight, in.Traj, in.By), true
	case InRecontract:
		return k.ProposeRecontract(in.Flight, in.Traj, in.By), true
	case InAck:
		k.Ack(in.Flight)
	case InRefuse:
		k.Refuse(in.Flight)
	case InFail:
		k.DeliveryFailed(in.Flight)
	case InTrack:
		k.UpdateTrack(in.Flight, in.Track)
	case InMarkIntruder:
		k.MarkIntruder(in.Flight)
	case InReanchor:
		k.ReanchorAll()
	case InRemove:
		k.Remove(in.Flight)
	case InTick:
		k.Tick()
	case InSetAuthority:
		k.SetAuthority(in.Flight, in.By)
	}
	return Verdict{}, false
}
