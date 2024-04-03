package components

import (
	"fmt"
	"slices"
	"strconv"
	"strings"

	"github.com/joeyak/scrum-poker/models"
)

var fibonacciSequence = []float64{1, 2, 3, 5, 8, 13, 21}

func userAnswer(cards map[string]string) string {
	var answers []string
	for row, card := range cards {
		answers = append(answers, fmt.Sprintf("%s:%s", row, card))
	}
	slices.Sort(answers)
	return strings.Join(answers, ", ")
}

func exitLink(session models.Session, user models.User) string {
	return fmt.Sprintf("/session/%s/user/%s/exit", session.ID, user.ID)
}

func trimFloat(f float64) string {
	return strings.TrimRight(strings.TrimRight(strconv.FormatFloat(f, 'f', 2, 64), "0"), ".")
}

func finalResultRange(f float64) string {
	last := 0.0
	current := fibonacciSequence[0]
	for _, seq := range fibonacciSequence[1:] {
		last = current
		current = seq
		if f < seq {
			break
		}
	}

	if f > current {
		return fmt.Sprintf("%s < X", trimFloat(f))
	}

	return fmt.Sprintf("%s - %s", trimFloat(last), trimFloat(current))
}
