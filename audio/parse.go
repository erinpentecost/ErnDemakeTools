package main

import (
	"bufio"
	"context"
	"encoding/csv"
	"fmt"
	"io"
	"log"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"sync/atomic"
	"syscall"
)

// Record holds the extracted fields from each block
type Record struct {
	Sex       string
	Race      string
	SoundFile string
	Response  string
}

func parseFile(path string) ([]Record, error) {
	file, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer file.Close()

	var records []Record
	var current Record
	inRecord := false

	sexRe := regexp.MustCompile(`Sex:(\w+)`)
	raceRe := regexp.MustCompile(`Race:(\w+)`)
	soundRe := regexp.MustCompile(`Sound_File:(.+)`)
	responseRe := regexp.MustCompile(`Response:\s*(.+)`)

	scanner := bufio.NewScanner(file)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())

		if strings.HasPrefix(line, "Record:") {
			// Start of a new record: push the old one if valid
			if inRecord {
				records = append(records, current)
			}
			current = Record{}
			inRecord = true
			continue
		}

		if matches := sexRe.FindStringSubmatch(line); matches != nil {
			current.Sex = strings.TrimSpace(strings.ToLower(matches[1]))
		}
		if matches := raceRe.FindStringSubmatch(line); matches != nil {
			current.Race = strings.TrimSpace(strings.ToLower(matches[1]))
		}
		if matches := soundRe.FindStringSubmatch(line); matches != nil {
			current.SoundFile = strings.TrimSpace(matches[1])
		}
		if matches := responseRe.FindStringSubmatch(line); matches != nil {
			current.Response = matches[1]
		}
	}

	// Append the last record
	if inRecord {
		records = append(records, current)
	}

	if err := scanner.Err(); err != nil {
		return nil, err
	}
	return records, nil
}

func normalizeRace(r string) string {
	return map[string]string{
		"a": "argonian",
		"b": "breton",
		"d": "dark elf",
		"h": "high elf",
		"i": "imperial",
		"k": "khajiit",
		"n": "nord",
		"o": "orc",
		"r": "redguard",
		"w": "wood elf",
	}[strings.ToLower(r)]
}

func normalizeSex(s string) string {
	return map[string]string{
		"f": "female",
		"m": "male",
	}[strings.ToLower(s)]
}

func csvFile(path string) ([]Record, error) {
	file, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer file.Close()

	var records []Record
	var current Record
	inRecord := false

	scanner := bufio.NewScanner(file)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" {
			continue
		}
		reader := csv.NewReader(strings.NewReader(line))
		// Read all records from the CSV string
		csvRecords, err := reader.ReadAll()
		if err != nil {
			return nil, fmt.Errorf("Error reading CSV: %q: %v", line, err)
		}
		if len(csvRecords[0]) < 2 {
			return nil, fmt.Errorf("expected at least 2 columns in %q", line)
		}

		rec := Record{
			SoundFile: csvRecords[0][0],
		}

		if len(csvRecords[0][1]) == 0 {
			return nil, fmt.Errorf("No subtitle record for %q", line)
		} else if strings.HasPrefix(csvRecords[0][1], "?") {
			rec.Response = getFill(csvRecords[0][1])
		} else {
			rec.Response = csvRecords[0][1]
		}

		if len(csvRecords[0]) > 2 {
			rec.Race = normalizeRace(csvRecords[0][2])
		}
		if len(csvRecords[0]) > 3 {
			rec.Sex = normalizeSex(csvRecords[0][3])
		}

		records = append(records, rec)
	}

	// Append the last record
	if inRecord {
		records = append(records, current)
	}

	if err := scanner.Err(); err != nil {
		return nil, err
	}
	return records, nil
}

func getVoice(r *Record) []string {
	// https://airdorf.fandom.com/wiki/Voice_Settings
	// Counterintuitively, higher values for Pitch and Speed will lower them both, while lower values will raise them.
	// Pitch - Can change Gender, Age, and Voice Deepness. Default is 64.
	// Speed - Rate of Speech. Default is 72.
	// Throat - Voice "nasality". Default is 128.
	// Mouth - Voice "stuffiness", also affects pitch. Default is 128.

	pitch := 64
	speed := 72
	throat := 128
	mouth := 128

	switch r.Race {
	case "argonian":
		// ok
		if r.Sex == "female" {
			pitch = 40
			speed = 56
			throat = 120
			mouth = 114
		} else {
			pitch = 63
			speed = 56
			throat = 120
			mouth = 114
		}
	case "breton":
		// ok
		if r.Sex == "female" {
			pitch = 40
			speed = 74
			throat = 128
			mouth = 50
		} else {
			pitch = 61
			speed = 72
			throat = 128
			mouth = 50
		}
	case "dark elf":
		// ok
		if r.Sex == "female" {
			pitch = 45
			speed = 72
			throat = 115
			mouth = 120
		} else {
			pitch = 64
			speed = 72
			throat = 105
			mouth = 110
		}
	case "high elf":
		// ok
		if r.Sex == "female" {
			pitch = 40
			speed = 68
			throat = 150
			mouth = 128
		} else {
			pitch = 64
			speed = 68
			throat = 150
			mouth = 128
		}
	case "imperial":
		// ok
		if r.Sex == "female" {
			pitch = 30
			speed = 72
			throat = 115
			mouth = 128
		} else {
			pitch = 64
			speed = 72
			throat = 105
			mouth = 128
		}
	case "khajiit":
		// ok
		if r.Sex == "female" {
			pitch = 40
			speed = 56
			throat = 165
			mouth = 116
		} else {
			pitch = 60
			speed = 56
			throat = 165
			mouth = 116
		}
	case "nord":
		// ok
		if r.Sex == "female" {
			pitch = 38
			speed = 72
			throat = 128
			mouth = 30
		} else {
			pitch = 64
			speed = 72
			throat = 128
			mouth = 30
		}
	case "orc":
		// ok
		if r.Sex == "female" {
			pitch = 60
			speed = 72
			throat = 97
			mouth = 130
		} else {
			pitch = 90
			speed = 72
			throat = 97
			mouth = 130
		}
	case "redguard":
		// ok
		if r.Sex == "female" {
			pitch = 43
			speed = 71
			throat = 145
			mouth = 145
		} else {
			pitch = 80
			speed = 72
			throat = 145
			mouth = 145
		}
	case "wood elf":
		// ok
		if r.Sex == "female" {
			pitch = 30
			speed = 65
			throat = 15
			mouth = 50
		} else {
			pitch = 40
			speed = 65
			throat = 15
			mouth = 50
		}
	}

	return []string{
		"-pitch", strconv.Itoa(pitch),
		"-speed", strconv.Itoa(speed),
		"-throat", strconv.Itoa(throat),
		"-mouth", strconv.Itoa(mouth),
	}

}

func main() {
	if len(os.Args) < 3 {
		fmt.Println("Usage: go run main.go <outdir> <inputfile....> ")
		return
	}

	outDir, err := filepath.Abs(os.Args[1])
	if err != nil {
		log.Fatalf("Error: The output path %q is messed up: %v", os.Args[1], err)
	}
	if err := os.Mkdir(outDir, os.ModePerm); err != nil {
		log.Fatalf("Error: Failed to make %q: %v", outDir, err)
	}

	dedupe := map[string]any{}

	var records []Record
	for _, f := range strings.Split(os.Args[2], ",") {
		var frecords []Record
		var err error
		if filepath.Ext(f) == ".csv" {
			frecords, err = csvFile(f)
		} else {
			frecords, err = parseFile(f)
		}

		if err != nil {
			fmt.Println("Error:", err)
			return
		}
		for _, r := range frecords {
			k := strings.ToLower(r.SoundFile)
			if _, present := dedupe[k]; !present {
				records = append(records, r)
				dedupe[k] = struct{}{}
			}
		}
	}

	// sox 'Dagoth Ur Welcome A.mp3' -C 10 -D -r 8000 'Dagoth Ur Welcome A crunchy.mp3' bass -6 100
	//
	tempDir, err := os.MkdirTemp(os.TempDir(), "vocode-")
	if err != nil {
		log.Fatalf("Error walking the making temp dir: %v", err)
	}
	defer os.RemoveAll(tempDir)

	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt, os.Kill, syscall.SIGTERM)
	defer cancel()

	var counter atomic.Int64

	for _, r := range records {
		if ctx.Err() != nil {
			break
		}
		err = func() error {
			id := counter.Add(1)
			//fmt.Printf("Record %d:\n", i+1)
			//fmt.Printf("  Sex: %s\n", r.Sex)
			//fmt.Printf("  Sound File: %s\n", r.SoundFile)
			//fmt.Printf("  Response: %s\n\n", r.Response)
			fmt.Printf("Processing file: %s\n", r.SoundFile)
			// make it work on windows
			r.SoundFile = strings.ToLower(r.SoundFile)
			outPath := filepath.Join(outDir, "Sound", strings.ReplaceAll(r.SoundFile, "\\", string(filepath.Separator)))
			// make sure directory is there
			if err := os.MkdirAll(filepath.Dir(outPath), os.ModePerm); err != nil {
				return fmt.Errorf("Failed to make output dir for %q: %v", outPath, err)
			}

			tmpFile := filepath.Join(tempDir, fmt.Sprintf("%d.wav", id))
			args := getVoice(&r)
			args = append(args, "-wav", tmpFile, r.Response)
			cmd := exec.Command("sam", args...)
			output, err := cmd.CombinedOutput()
			if err != nil {
				return fmt.Errorf("Command %q failed: %v\nOutput: %s\n", cmd.String(), err, output)
			}
			// at this point, we have a temp wav file.
			switch filepath.Ext(strings.ToLower(r.SoundFile)) {
			case ".wav":
				// always convert to mp3
				outPath = outPath[:len(outPath)-4] + ".mp3"
				fallthrough
			case ".mp3":
				if err := toMP3(tmpFile, outPath); err != nil {
					return fmt.Errorf("%q: %v", outPath, err)
				}
			default:
				return fmt.Errorf("unknown format for file %q", r.SoundFile)
			}
			return nil
		}()
		if err != nil {
			fmt.Printf("ERROR!!!! %v", err)
			os.Exit(4)
		}
	}
}

func copyFile(sourcePath, destPath string) error {
	inputFile, err := os.Open(sourcePath)
	if err != nil {
		return fmt.Errorf("couldn't open source file: %w", err)
	}
	defer inputFile.Close()

	outputFile, err := os.Create(destPath)
	if err != nil {
		return fmt.Errorf("couldn't create dest file: %w", err)
	}
	defer outputFile.Close()

	if _, err = io.Copy(outputFile, inputFile); err != nil {
		return fmt.Errorf("copy failed: %w", err)
	}
	fmt.Printf("Finished copying to %q", destPath)
	return nil
}

func toMP3(inFile string, outPath string) error {
	//cmd := exec.Command("sox", "-r", "12000", inFile, outPath)
	cmd := exec.Command("sox", inFile, outPath)
	output, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("Command %q failed: %v\nOutput: %s\n", cmd.String(), err, output)
	}
	return nil
}
