package world

import "math"

// openSimplexNoise implements 2D OpenSimplex-like noise for terrain generation.
// Uses a permutation table and smooth interpolation.
type openSimplexNoise struct {
	perm [512]int
}

// NewNoise creates a new noise generator with the given seed.
func NewNoise(seed int64) *openSimplexNoise {
	n := &openSimplexNoise{}
	// Simple PRNG to fill permutation table
	var s uint64 = uint64(seed)
	// Use xorshift64
	p := make([]int, 256)
	for i := 0; i < 256; i++ {
		p[i] = i
	}
	// Fisher-Yates shuffle with xorshift64 PRNG
	for i := 255; i > 0; i-- {
		s ^= s << 13
		s ^= s >> 7
		s ^= s << 17
		j := int(s % uint64(i+1))
		p[i], p[j] = p[j], p[i]
	}
	for i := 0; i < 256; i++ {
		n.perm[i] = p[i]
		n.perm[i+256] = p[i]
	}
	return n
}

func (n *openSimplexNoise) grad(hash int, x, y float64) float64 {
	h := hash & 7
	u := 0.0
	v := 0.0
	if h < 4 {
		u = x
	} else {
		u = y
	}
	if h == 0 || h == 1 || h == 4 || h == 5 {
		v = y
	} else {
		v = x
	}
	if h&1 != 0 {
		u = -u
	}
	if h&2 != 0 {
		v = -v
	}
	return u + v
}

func fade(t float64) float64 {
	return t * t * t * (t*(t*6-15) + 10)
}

func lerp(a, b, t float64) float64 {
	return a + t*(b-a)
}

// Noise2D returns a noise value between -1 and 1 at (x, y).
// Higher frequency implies more zoomed-in detail; adjust by scaling x,y inputs.
func (n *openSimplexNoise) Noise2D(x, y float64) float64 {
	// Determine grid cell
	X := int(math.Floor(x)) & 255
	Y := int(math.Floor(y)) & 255

	x -= math.Floor(x)
	y -= math.Floor(y)

	u := fade(x)
	v := fade(y)

	a := n.perm[n.perm[X]+Y]
	b := n.perm[n.perm[X+1]+Y]
	c := n.perm[n.perm[X]+Y+1]
	d := n.perm[n.perm[X+1]+Y+1]

	return lerp(
		lerp(n.grad(a, x, y), n.grad(b, x-1, y), u),
		lerp(n.grad(c, x, y-1), n.grad(d, x-1, y-1), u),
		v,
	)
}

// OctaveNoise sums multiple octaves of noise for richer terrain.
func (n *openSimplexNoise) OctaveNoise(x, y float64, octaves int, persistence, lacunarity float64) float64 {
	total := 0.0
	amplitude := 1.0
	frequency := 1.0
	maxValue := 0.0

	for i := 0; i < octaves; i++ {
		total += n.Noise2D(x*frequency, y*frequency) * amplitude
		maxValue += amplitude
		amplitude *= persistence
		frequency *= lacunarity
	}

	return total / maxValue
}
