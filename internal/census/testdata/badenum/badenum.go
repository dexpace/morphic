// Package badenum declares a constant of a kind no census counts.
package badenum

// Ratio is a floating-point type with a constant.
type Ratio float64

// Half is the constant a census cannot count a value of.
const Half Ratio = 0.5
