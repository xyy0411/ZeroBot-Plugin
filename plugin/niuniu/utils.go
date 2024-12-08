// Package niuniu 牛牛大作战
package niuniu

import (
	"math/rand"
)

func randomChoice(options []string) string {
	return options[rand.Intn(len(options))]
}
