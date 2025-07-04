package parser

import (
	"bufio"
	"bytes"
	"fmt"
	"io"
	"regexp"
	"strconv"
	"strings"

	"github.com/anyproto/goru/pkg/model"
)

var (
	goroutineHeaderRe = regexp.MustCompile(`^goroutine (\d+) \[([\w\s,]+?)(?:, (\d+ minutes?))?\]:$`)
	stackFrameRe      = regexp.MustCompile(`^(.+?)\(.*?\)$`)
	fileLineRe        = regexp.MustCompile(`^\s+(.+?):(\d+)(?:\s|$)`)
	createdByRe       = regexp.MustCompile(`^created by (.+)$`)
	createdAtRe       = regexp.MustCompile(`^\s+(.+?):(\d+)(?:\s|$)`)
	
	// Regexes for extractFunctionName
	funcRe = regexp.MustCompile(`^([^(]+(?:\(\*[^)]+\))?[^(]*)(?:\(|$)`)
	
	// Regexes for stripMemoryAddresses
	ptrRe = regexp.MustCompile(`\((0x[0-9a-fA-F]+(?:,\s*0x[0-9a-fA-F]+)*(?:,\s*[^)]+)*)\)`)
	hexRe = regexp.MustCompile(`0x[0-9a-fA-F]+`)
)

type Parser struct {
	stripAddresses bool
}

func New() *Parser {
	return &Parser{
		stripAddresses: true,
	}
}

func (p *Parser) Parse(r io.Reader, host string) (*model.Snapshot, error) {
	snapshot := model.NewSnapshot(host)
	scanner := bufio.NewScanner(r)

	var currentState model.GoroutineState
	var currentWait string
	var currentStack []model.StackFrame
	var currentCreatedBy *model.StackFrame
	var inGoroutine bool

	for scanner.Scan() {
		line := scanner.Text()

		// Check for goroutine header
		if matches := goroutineHeaderRe.FindStringSubmatch(line); matches != nil {
			// Save previous goroutine if any
			if inGoroutine && len(currentStack) > 0 {
				snapshot.AddGoroutine(currentState, currentStack, currentWait, currentCreatedBy)
			}

			// Start new goroutine
			inGoroutine = true
			currentState = p.parseState(matches[2])
			currentWait = matches[3]
			currentStack = nil
			currentCreatedBy = nil
			continue
		}

		if !inGoroutine {
			continue
		}

		// Empty line ends the goroutine
		if line == "" {
			if len(currentStack) > 0 {
				snapshot.AddGoroutine(currentState, currentStack, currentWait, currentCreatedBy)
			}
			inGoroutine = false
			continue
		}

		// Check for "created by" line
		if matches := createdByRe.FindStringSubmatch(line); matches != nil {
			// Extract the function name that created this goroutine
			createdByFunc := matches[1]

			// Remove "in goroutine X" suffix if present
			if idx := strings.Index(createdByFunc, " in goroutine "); idx > 0 {
				createdByFunc = createdByFunc[:idx]
			}

			// Next line should have file:line
			if scanner.Scan() {
				fileLine := scanner.Text()
				if fileMatches := fileLineRe.FindStringSubmatch(fileLine); fileMatches != nil {
					lineNum, _ := strconv.Atoi(fileMatches[2])
					currentCreatedBy = &model.StackFrame{
						Func: p.extractFunctionName(createdByFunc),
						File: fileMatches[1],
						Line: lineNum,
					}
				}
			}
			continue
		}

		// Skip standalone file:line after created by (already processed)
		if createdAtRe.MatchString(line) {
			continue
		}

		// Check for stack frame function
		if !strings.HasPrefix(line, "\t") && !strings.HasPrefix(line, " ") {
			// This line contains a function name
			// Next line should have file:line
			if scanner.Scan() {
				fileLine := scanner.Text()
				if matches := fileLineRe.FindStringSubmatch(fileLine); matches != nil {
					funcName := p.extractFunctionName(line)
					lineNum, _ := strconv.Atoi(matches[2])
					currentStack = append(currentStack, model.StackFrame{
						Func: funcName,
						File: matches[1],
						Line: lineNum,
					})
				}
			}
		}
	}

	// Handle last goroutine if file doesn't end with empty line
	if inGoroutine && len(currentStack) > 0 {
		snapshot.AddGoroutine(currentState, currentStack, currentWait, currentCreatedBy)
	}

	if err := scanner.Err(); err != nil {
		return nil, fmt.Errorf("scanning input: %w", err)
	}

	return snapshot, nil
}

func (p *Parser) parseState(stateStr string) model.GoroutineState {
	// Clean up the state string
	stateStr = strings.TrimSpace(stateStr)
	stateStr = strings.Split(stateStr, ",")[0]

	switch {
	case stateStr == "running":
		return model.StateRunning
	case stateStr == "runnable":
		return model.StateRunnable
	case stateStr == "syscall":
		return model.StateSyscall
	case stateStr == "chan receive", stateStr == "chan send", stateStr == "select":
		return model.StateBlocked
	case stateStr == "IO wait", stateStr == "semacquire", stateStr == "sync.Cond.Wait":
		return model.StateWaiting
	default:
		// Many states like "sleep", "finalizer wait" etc. map to waiting
		return model.StateWaiting
	}
}

func (p *Parser) extractFunctionName(line string) string {
	// Extract function name before the arguments parentheses
	line = strings.TrimSpace(line)

	// Use the pre-compiled regex to handle method receivers
	// Match everything up to the first '(' that isn't part of a receiver
	if matches := funcRe.FindStringSubmatch(line); len(matches) > 1 {
		return strings.TrimSpace(matches[1])
	}

	// Fallback: find first parenthesis
	if idx := strings.Index(line, "("); idx > 0 {
		return strings.TrimSpace(line[:idx])
	}

	return line
}

func (p *Parser) stripMemoryAddresses(s string) string {
	// Strip pointer values in function arguments first using pre-compiled regex
	if ptrRe.MatchString(s) {
		s = ptrRe.ReplaceAllString(s, "(...)")
	}

	// Strip standalone hex addresses like 0x123abc using pre-compiled regex
	s = hexRe.ReplaceAllString(s, "0x?")

	return s
}

func (p *Parser) ParseBytes(data []byte, host string) (*model.Snapshot, error) {
	return p.Parse(bytes.NewReader(data), host)
}
