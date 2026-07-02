package autoresearch

import (
	cryptorand "crypto/rand"
	"encoding/binary"
	"log"
	"math"
	"math/rand" // nosemgrep: go.lang.security.audit.crypto.math_random.math-random-used -- statistical sampling only; seeded from crypto/rand
	"sync"
)

// BayesSampler maintains per-class Beta priors and Thompson-samples the next
// experiment class. Updates are append-only, so the posterior becomes
// progressively sharper as outcomes accumulate.
//
// Beta(α, β):
//
//	α grows by 1 on a kept (success) outcome
//	β grows by 1 on a discarded/tripwired (failure) outcome
//
// Thompson sampling: draw one Beta(α_c, β_c) per class, pick the class
// whose draw is highest. Naturally explores under-tried classes (wide
// posterior → variable draws) while exploiting confident winners.
type BayesSampler struct {
	mu         sync.Mutex
	priors     map[string]*betaPrior // key = repo|class
	classes    []ExperimentClass
	priorAlpha float64
	priorBeta  float64
	rng        *rand.Rand
}

type betaPrior struct {
	alpha float64
	beta  float64
}

// NewBayesSampler constructs a sampler. Pass classes as nil to use DefaultClasses.
// PriorAlpha and PriorBeta default to 1.0 (uniform) when zero.
func NewBayesSampler(classes []ExperimentClass, priorAlpha, priorBeta float64) *BayesSampler {
	if len(classes) == 0 {
		classes = DefaultClasses
	}
	if priorAlpha <= 0 {
		priorAlpha = 1.0
	}
	if priorBeta <= 0 {
		priorBeta = 1.0
	}
	return &BayesSampler{
		priors:     make(map[string]*betaPrior),
		classes:    append([]ExperimentClass(nil), classes...),
		priorAlpha: priorAlpha,
		priorBeta:  priorBeta,
		rng:        rand.New(rand.NewSource(secureSeed())), // #nosec G404 -- Thompson sampling needs statistical, not cryptographic, randomness; the seed itself comes from crypto/rand (secureSeed)
	}
}

// secureSeed draws 8 bytes from crypto/rand and returns them as int64.
// Replaces the previous time.Now().UnixNano() seed, which an observer
// could predict from a known process start time. Falls back to a non-zero
// constant if the OS RNG is unavailable (vanishingly rare; better than
// panicking the sampler at startup).
func secureSeed() int64 {
	var b [8]byte
	if _, err := cryptorand.Read(b[:]); err != nil {
		// crypto/rand.Read returning an error is extraordinarily rare on
		// Unix (it would mean /dev/urandom is gone) but if it does, log
		// loudly. The constant-1 fallback prevents the sampler from
		// panicking at startup, but it also means Thompson sampling
		// becomes fully deterministic for this process — operators need
		// to know that has happened so they can investigate the host RNG.
		log.Printf("[autoresearch] CRITICAL: crypto/rand.Read failed: %v — falling back to deterministic seed; Thompson sampling will be predictable for this process", err)
		return 1
	}
	// #nosec G115 -- b is 8 random bytes; the uint64→int64 wraparound is harmless because only the bit pattern matters for a seed.
	return int64(binary.LittleEndian.Uint64(b[:]))
}

// SetSeed makes Thompson sampling deterministic for tests.
func (s *BayesSampler) SetSeed(seed int64) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.rng = rand.New(rand.NewSource(seed)) // #nosec G404 -- deterministic test seeding by design
}

// Classes returns the class set this sampler covers.
func (s *BayesSampler) Classes() []ExperimentClass {
	return append([]ExperimentClass(nil), s.classes...)
}

// Next Thompson-samples the next class for the given repo.
func (s *BayesSampler) Next(repo string) ExperimentClass {
	s.mu.Lock()
	defer s.mu.Unlock()

	var best ExperimentClass
	var bestSample float64 = -1
	for _, c := range s.classes {
		p := s.priorFor(repo, c)
		sample := sampleBeta(s.rng, p.alpha, p.beta)
		if sample > bestSample {
			bestSample = sample
			best = c
		}
	}
	return best
}

// Update records the outcome of an experiment for the given (repo, class).
// `kept=true` increments alpha (success), `kept=false` increments beta.
// Callers must skip this entirely for infra-caused failures.
func (s *BayesSampler) Update(repo string, class ExperimentClass, kept bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	p := s.priorFor(repo, class)
	if kept {
		p.alpha++
	} else {
		p.beta++
	}
}

// Posterior returns the current Beta(α, β) for the (repo, class) pair.
// Useful for `vxd autoresearch status` and tests.
func (s *BayesSampler) Posterior(repo string, class ExperimentClass) (alpha, beta float64) {
	s.mu.Lock()
	defer s.mu.Unlock()
	p := s.priorFor(repo, class)
	return p.alpha, p.beta
}

// Mean returns the posterior mean α/(α+β) — a "smoothed kept-rate".
func (s *BayesSampler) Mean(repo string, class ExperimentClass) float64 {
	a, b := s.Posterior(repo, class)
	if a+b == 0 {
		return 0
	}
	return a / (a + b)
}

func (s *BayesSampler) priorFor(repo string, class ExperimentClass) *betaPrior {
	key := repo + "|" + string(class)
	if p, ok := s.priors[key]; ok {
		return p
	}
	p := &betaPrior{alpha: s.priorAlpha, beta: s.priorBeta}
	s.priors[key] = p
	return p
}

// sampleBeta returns a draw from Beta(alpha, beta) using the
// gamma-ratio trick: X = G(α,1)/(G(α,1)+G(β,1)) ~ Beta(α,β).
//
// Uses Marsaglia & Tsang's "Generating gamma variates" algorithm for the
// gamma draws. Handles α<1 via the boost trick.
func sampleBeta(r *rand.Rand, alpha, beta float64) float64 {
	x := sampleGamma(r, alpha)
	y := sampleGamma(r, beta)
	if x+y == 0 {
		return 0
	}
	return x / (x + y)
}

// sampleGamma draws from Gamma(shape, 1) using Marsaglia & Tsang's algorithm.
// Handles shape<1 via the boost trick: G(α,1) = G(α+1,1) · U^(1/α).
func sampleGamma(r *rand.Rand, shape float64) float64 {
	if shape < 1 {
		u := r.Float64()
		return sampleGamma(r, shape+1) * math.Pow(u, 1.0/shape)
	}
	d := shape - 1.0/3.0
	c := 1.0 / (3.0 * math.Sqrt(d))
	for {
		var x, v float64
		for {
			x = r.NormFloat64()
			v = 1 + c*x
			if v > 0 {
				break
			}
		}
		v = v * v * v
		u := r.Float64()
		if u < 1-0.0331*x*x*x*x {
			return d * v
		}
		if math.Log(u) < 0.5*x*x+d*(1-v+math.Log(v)) {
			return d * v
		}
	}
}
