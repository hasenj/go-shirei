package slay

// taken from https://github.com/alessani/ColorConverter/blob/master/ColorSpaceUtilities.h
func FloatHSLToRGB(h float64, s float64, l float64) (float64, float64, float64) {
	// Check for saturation. If there isn't any just return the luminance value for each, which results in gray.
	if s == 0.0 {
		return l, l, l
	}

	var temp2 float64
	// Test for luminance and compute temporary values based on luminance and saturation
	if l < 0.5 {
		temp2 = l * (1.0 + s)
	} else {
		temp2 = l + s - l*s
	}
	temp1 := 2.0*l - temp2

	// Compute intermediate values based on hue
	temp := [3]float64{
		h + 1.0/3.0,
		h,
		h - 1.0/3.0,
	}

	for i := 0; i < 3; i++ {
		// Adjust the range
		if temp[i] < 0.0 {
			temp[i] += 1.0
		}
		if temp[i] > 1.0 {
			temp[i] -= 1.0
		}

		if 6.0*temp[i] < 1.0 {
			temp[i] = temp1 + (temp2-temp1)*6.0*temp[i]
		} else {
			if 2.0*temp[i] < 1.0 {
				temp[i] = temp2
			} else {
				if 3.0*temp[i] < 2.0 {
					temp[i] = temp1 + (temp2-temp1)*((2.0/3.0)-temp[i])*6.0
				} else {
					temp[i] = temp1
				}
			}
		}
	}

	return temp[0], temp[1], temp[2]
}
