// LTTB (Largest-Triangle-Three-Buckets) downsampling for time-series.
//
// Preserves visual peaks far better than averaging or decimation, and is O(n).
// Design §F4: when a window holds more points than N (default 300), apply LTTB.
//
// Acceptance (F4-01): output length at most N; first and last points preserved;
// global max within a bucket retained.
//
// Ported from the standard LTTB algorithm (Sveinn Steinarsson, 2013,
// "Downsampling Time Series for Visual Representation").

package api

// lttbDownsample reduces samples to at most threshold points using LTTB.
// Returns the input unchanged if len(samples) <= threshold. The first and
// last points are always preserved.
func lttbDownsample[T any](samples []T, threshold int, xOf func(T) float64, yOf func(T) float64) []T {
	n := len(samples)
	if threshold >= n || threshold < 3 {
		return samples
	}

	out := make([]T, 0, threshold)

	// Always include the first point.
	out = append(out, samples[0])

	// Bucket size. We divide the data (excluding first and last) into
	// threshold-2 buckets, then pick one point per bucket.
	bucketSize := float64(n-2) / float64(threshold-2)

	for i := 0; i < threshold-2; i++ {
		// Bucket boundaries.
		bucketStart := int(float64(i)*bucketSize) + 1
		bucketEnd := int(float64(i+1)*bucketSize) + 1
		if bucketEnd > n-1 {
			bucketEnd = n - 1
		}
		if bucketStart >= bucketEnd {
			bucketStart = bucketEnd - 1
			if bucketStart < 1 {
				bucketStart = 1
			}
		}

		// Previous selected point (the one we appended last).
		prev := out[len(out)-1]
		px, py := xOf(prev), yOf(prev)

		// Average of the NEXT bucket (used to compute the triangle area).
		nextBucketStart := int(float64(i+1)*bucketSize) + 1
		nextBucketEnd := int(float64(i+2)*bucketSize) + 1
		if nextBucketEnd > n-1 {
			nextBucketEnd = n - 1
		}
		if nextBucketStart >= nextBucketEnd {
			nextBucketStart = nextBucketEnd - 1
		}
		var avgX, avgY float64
		cnt := 0
		for j := nextBucketStart; j < nextBucketEnd; j++ {
			avgX += xOf(samples[j])
			avgY += yOf(samples[j])
			cnt++
		}
		if cnt > 0 {
			avgX /= float64(cnt)
			avgY /= float64(cnt)
		}

		// Find the point in the current bucket that forms the largest triangle
		// with prev and (avgX, avgY).
		maxArea := -1.0
		maxIdx := bucketStart
		for j := bucketStart; j < bucketEnd; j++ {
			x, y := xOf(samples[j]), yOf(samples[j])
			// Triangle area = 0.5 * |x*(py-avgY) + px*(avgY-y) + avgX*(y-py)|
			area := 0.5 * absF64(x*(py-avgY)+px*(avgY-y)+avgX*(y-py))
			if area > maxArea {
				maxArea = area
				maxIdx = j
			}
		}
		out = append(out, samples[maxIdx])
	}

	// Always include the last point.
	out = append(out, samples[n-1])
	return out
}

func absF64(v float64) float64 {
	if v < 0 {
		return -v
	}
	return v
}
