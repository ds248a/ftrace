package ftrace

import (
	"errors"
	"fmt"
	"strings"
	"sync"
)

const (
	maxArguments      = 16
	enabledStatusFile = "/proc/sys/kernel/ftrace_enabled"
	systemProbesFile  = "/sys/kernel/debug/tracing/kprobe_events"
	eventsPipeFile    = "/sys/kernel/debug/tracing/trace_pipe"
	probeFileFormat   = "/sys/kernel/debug/tracing/events/kprobes/%s/enable"
	eventFileFormat   = "/sys/kernel/debug/tracing/events/%s/enable"
)

var errUnavailable = errors.New("FTRACE kernel framework not available on your system")

// ------------------------
//   Probe
// ------------------------

// Probe represents a FTRACE probe to a system call and optional sub events.
type Probe struct {
	sync.RWMutex
	name       string            // custom name of the probe
	fileName   string            // probe status file name
	syscall    string            // syscall to intercept
	descriptor string            // ftrace descriptor of the probe
	events     map[string]string // kernel sub events
	enabled    bool              // probe status
	pipe       chan string       // pipe file reader
	done       chan bool         // channel used to signal from the worker
	bus        chan Event        // channel where events are sent
}

// NewProbe creates a new probe with a custom name for the given syscall and optional sub events.
func NewProbe(name string, syscall string, subEvents []string) *Probe {
	return &Probe{
		name:       name,
		fileName:   fmt.Sprintf(probeFileFormat, name),
		syscall:    syscall,
		descriptor: makeDescriptor(name, syscall),
		events:     mapSubevents(subEvents),
		enabled:    false,
		pipe:       nil,
		done:       make(chan bool),
		bus:        make(chan Event),
	}
}

// Enabled return true if this probe is enabled and running, otherwise false.
func (p *Probe) Enabled() bool {
	p.RLock()
	defer p.RUnlock()
	return p.enabled
}

// Events returns a channel where FTRACE events will be written by this Probe worker routine.
func (p *Probe) Events() <-chan Event {
	return p.bus
}

func (p *Probe) selectEvent(event string) bool {
	// main probe event
	if strings.Contains(event, p.name) {
		return true
	}
	// one of the sub events
	for eventName := range p.events {
		if strings.Contains(event, eventName) {
			return true
		}
	}
	return false
}

//
func (p *Probe) worker() {
	// signal we're done when we exit
	defer func() {
		p.done <- true
	}()

	for eventLine := range p.pipe {
		// our parent go routine is telling us to quit
		if eventLine == "<quit>" {
			break
		}

		// check if we're interested in this event
		if p.selectEvent(eventLine) {
			// parse the raw event data
			if event, err := parseEvent(eventLine); err != nil {
				fmt.Printf("Error while parsing event: %s\n", err)
			} else {
				p.bus <- event
			}
		}
	}
}

// Enable enables this probe and starts its async worker routine in order to read FTRACE events.
func (p *Probe) Enable() (err error) {
	p.Lock()
	defer p.Unlock()

	if p.enabled == true {
		return nil
	}

	if Available() == false {
		return errUnavailable
	}

	// enable all events
	for eventName, eventFileName := range p.events {
		if err = writeFile(eventFileName, "1"); err != nil {
			return fmt.Errorf("Error while enabling event %s: %s", eventName, err)
		}
	}

	// create the custom kprobe consumer
	if err = writeFile(systemProbesFile, p.descriptor); err != nil {
		return fmt.Errorf("Error while enabling probe descriptor for %s: %s", p.name, err)
	}

	// enable the probe
	if err = writeFile(p.fileName, "1"); err != nil {
		return fmt.Errorf("Error while enable probe %s: %s", p.name, err)
	}

	// create the handle to the pipe file
	if p.pipe, err = asyncFileReader(eventsPipeFile); err != nil {
		return fmt.Errorf("Error while opening %s: %s", eventsPipeFile, err)
	}

	p.enabled = true

	// start the async worker that will read events from the
	// pipe file and send them to the `bus` channel
	go p.worker()

	return nil
}

// Reset disables this probe.
func (p *Probe) Reset() error {
	// disable all events
	for eventName, eventFileName := range p.events {
		if err := writeFile(eventFileName, "0"); err != nil {
			return fmt.Errorf("Error while disabling event %s: %s", eventName, err)
		}
	}

	// disable the probe itself
	if err := writeFile(p.fileName, "0"); err != nil {
		return fmt.Errorf("Error while disabling probe %s: %s", p.name, err)
	}

	// remove the probe from the system
	if err := appendFile(systemProbesFile, fmt.Sprintf("-:%s", p.name)); err != nil {
		return fmt.Errorf("Error while removing the probe %s: %s", p.name, err)
	}

	return nil
}

// Disable disables this probe and stops its async worker.
func (p *Probe) Disable() error {
	p.Lock()
	defer p.Unlock()

	if p.enabled == false {
		return nil
	}

	if err := p.Reset(); err != nil {
		return err
	}

	p.enabled = false
	p.pipe <- "<quit>"

	// wait for the worker to finish
	<-p.done

	return nil
}
