package collector

import (
	"context"
	"fmt"
	"golang.org/x/term"
	"math"
	"os"
	"sync"
	"time"
)

type CatalogProgressStatus string

// ANSI escape codes
const (
	CursorUp       = "\x1b[%dA"
	ClearLine      = "\x1b[2K"
	CarriageReturn = "\r"

	StatusInitializing            CatalogProgressStatus = "Initializing"
	StatusUnpacking               CatalogProgressStatus = "Unpacking..."
	StatusCollectingBundleMetrics CatalogProgressStatus = "Collecting Bundle Metrics"
	StatusFailed                  CatalogProgressStatus = "Failed"
	StatusDone                    CatalogProgressStatus = "Done"
)

type CatalogProgress struct {
	catalogName      string
	status           CatalogProgressStatus
	totalBundles     int
	bundlesProcessed int
	numErrors        int
	curError         error
}

func (c *CatalogProgress) CatalogName() string {
	return c.catalogName
}

func (c *CatalogProgress) Status() CatalogProgressStatus {
	return c.status
}

func (c *CatalogProgress) TotalBundles() int {
	return c.totalBundles
}

func (c *CatalogProgress) BundlesProcessed() int {
	return c.bundlesProcessed
}

func (c *CatalogProgress) NumErrors() int {
	return c.numErrors
}

func (c *CatalogProgress) CurError() error {
	return c.curError
}

type ProgressRenderer func(ctx context.Context, p *Progressometer, stop <-chan struct{}) <-chan struct{}

type Progressometer struct {
	progMap               map[string]*CatalogProgress
	render                ProgressRenderer
	catalogOrder          []string
	totalBundlesProcessed int
	timeStart             time.Time
	m                     sync.RWMutex
	done                  chan struct{}
	renderFinished        <-chan struct{}
}

func NewProgressometer() *Progressometer {
	return &Progressometer{
		progMap: make(map[string]*CatalogProgress),
		render:  SimpleProgressRenderer,
		m:       sync.RWMutex{},
	}
}

func (p *Progressometer) WalkProgress(do func(catalogName string, progress *CatalogProgress)) {
	p.m.RLock()
	defer p.m.RUnlock()
	for _, catalogName := range p.catalogOrder {
		do(catalogName, p.progMap[catalogName])
	}
}

func (p *Progressometer) AddNewCatalog(catalogName string) {
	p.m.Lock()
	defer p.m.Unlock()
	if _, ok := p.progMap[catalogName]; !ok {
		p.progMap[catalogName] = &CatalogProgress{
			catalogName: catalogName,
			status:      StatusInitializing,
		}
		p.catalogOrder = append(p.catalogOrder, catalogName)
	}
}

func (p *Progressometer) NotifyUnpacking(catalogName string) {
	p.m.Lock()
	defer p.m.Unlock()
	if _, ok := p.progMap[catalogName]; ok {
		p.progMap[catalogName].status = StatusUnpacking
	}
}

func (p *Progressometer) NotifyUnpacked(catalogName string, numBundles int) {
	p.m.Lock()
	defer p.m.Unlock()
	if _, ok := p.progMap[catalogName]; ok {
		p.progMap[catalogName].status = StatusCollectingBundleMetrics
		p.progMap[catalogName].totalBundles = numBundles
	}
}

func (p *Progressometer) NotifyError(catalogName string, err error) {
	p.m.Lock()
	defer p.m.Unlock()
	if _, ok := p.progMap[catalogName]; ok {
		p.progMap[catalogName].curError = err
		p.progMap[catalogName].numErrors++
		p.progMap[catalogName].bundlesProcessed++
	}
}

func (p *Progressometer) NotifyFailed(catalogName string, err error) {
	p.m.Lock()
	defer p.m.Unlock()
	if _, ok := p.progMap[catalogName]; ok {
		p.progMap[catalogName].curError = err
		p.progMap[catalogName].numErrors++
		p.progMap[catalogName].status = StatusFailed
	}
}

func (p *Progressometer) NotifyBundleMetricsGathered(catalogName string) {
	p.m.Lock()
	defer p.m.Unlock()
	if _, ok := p.progMap[catalogName]; ok {
		p.progMap[catalogName].bundlesProcessed++
		if p.progMap[catalogName].bundlesProcessed == p.progMap[catalogName].totalBundles {
			p.progMap[catalogName].status = StatusDone
		}
	}
}

func (p *Progressometer) Start(ctx context.Context) {
	p.m.Lock()
	defer p.m.Unlock()
	if p.done != nil {
		return
	}
	p.done = make(chan struct{})
	p.renderFinished = p.render(ctx, p, p.done)
}

func (p *Progressometer) Done() {
	p.m.RLock()
	defer p.m.RUnlock()
	close(p.done)
	<-p.renderFinished
	p.done = nil
	p.renderFinished = nil
}

func SimpleProgressRenderer(ctx context.Context, p *Progressometer, stop <-chan struct{}) <-chan struct{} {
	done := make(chan struct{})
	go func() {
		defer close(done)
		ticker := time.NewTicker(100 * time.Millisecond)
		start := time.Now()
		out := output{}
		fmt.Printf("Collecting bundle metrics...\n\n")
		for {
			select {
			case <-ctx.Done():
				break
			case <-ticker.C:
				out.Clear()
				totalProcessed := 0
				p.WalkProgress(func(catalogName string, progress *CatalogProgress) {
					totalProcessed += progress.BundlesProcessed()
					completion := 0.
					details := ""
					if progress.TotalBundles() > 0 {
						completion = 100 * float64(progress.BundlesProcessed()) / float64(progress.TotalBundles())
						details = fmt.Sprintf(" (%.2f%% - %d errors)", completion, progress.NumErrors())
					}
					if progress.Status() == StatusFailed {
						details = progress.CurError().Error()
					}
					out.WriteLine(fmt.Sprintf("%s: %s%s", catalogName, progress.Status(), details))
				})
				out.WriteLine("")
				rate := fmt.Sprintf("%.2f", math.Floor(float64(totalProcessed)/time.Since(start).Seconds()))
				out.WriteLine(fmt.Sprintf("Total: %d | Elapsed: %.2f sec | Rate: %s bundles/sec", totalProcessed, time.Since(start).Seconds(), rate))
				out.Print()
				select {
				case <-stop:
					return
				default:
					continue
				}
			}
		}
	}()
	return done
}

type output struct {
	lines []string
}

func (o *output) WriteLine(line string) {
	o.lines = append(o.lines, line)
}

func (o *output) Clear() {
	fmt.Printf(CursorUp, len(o.lines))
	o.lines = nil
}

func (o *output) Print() {
	width, _, _ := term.GetSize(int(os.Stdout.Fd()))
	for _, line := range o.lines {
		if len(line)+1 > width && width > 0 {
			line = line[:len(line)-3] + "..."
		}
		fmt.Printf("%s%s%s\n", CarriageReturn, ClearLine, line)
	}
}
