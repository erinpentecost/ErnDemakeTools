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

		if strings.HasPrefix(csvRecords[0][1], "?") {
			rec.Response = getFill(csvRecords[0][1])
		} else {
			rec.Response = csvRecords[0][1]
		}

		if len(csvRecords[0]) > 2 {
			rec.Race = normalizeRace(csvRecords[0][2])
		}
		if len(csvRecords[0]) > 3 {
			rec.Race = normalizeSex(csvRecords[0][3])
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
	// // these all sound awful
	switch r.Race {
	case "argonian":
		if r.Sex == "female" {
			// too deep
			return []string{"-pitch", "44", "-speed", "72", "-throat", "60", "-mouth", "132"}
		} else {
			// tf?
			return []string{"-pitch", "68", "-speed", "72", "-throat", "64", "-mouth", "128"}
		}
	case "breton":
		if r.Sex == "female" {
			// good? no
			return []string{"-pitch", "40", "-speed", "74", "-throat", "120", "-mouth", "128"}
		} else {
			// way too deep
			return []string{"-pitch", "64", "-speed", "74", "-throat", "120", "-mouth", "129"}
		}
	case "dark elf":
		if r.Sex == "female" {
			return []string{"-pitch", "45", "-speed", "72", "-throat", "92", "-mouth", "116"}
		} else {
			return []string{"-pitch", "66", "-speed", "72", "-throat", "96", "-mouth", "110"}
		}
	case "high elf":
		if r.Sex == "female" {
			return []string{"-pitch", "40", "-speed", "74", "-throat", "110", "-mouth", "150"}
		} else {
			return []string{"-pitch", "64", "-speed", "74", "-throat", "110", "-mouth", "150"}
		}
	case "imperial":
		if r.Sex == "female" {
			return []string{"-pitch", "40", "-speed", "72", "-throat", "124", "-mouth", "124"}
		} else {
			return []string{"-pitch", "64", "-speed", "72", "-throat", "128", "-mouth", "120"}
		}
	case "khajiit":
		if r.Sex == "female" {
			return []string{"-pitch", "40", "-speed", "72", "-throat", "108", "-mouth", "144"}
		} else {
			return []string{"-pitch", "64", "-speed", "72", "-throat", "112", "-mouth", "140"}
		}
	case "nord":
		if r.Sex == "female" {
			return []string{"-pitch", "40", "-speed", "72", "-throat", "144", "-mouth", "122"}
		} else {
			return []string{"-pitch", "64", "-speed", "72", "-throat", "150", "-mouth", "118"}
		}
	case "orc":
		if r.Sex == "female" {
			return []string{"-pitch", "60", "-speed", "72", "-throat", "110", "-mouth", "105"}
		} else {
			return []string{"-pitch", "72", "-speed", "72", "-throat", "110", "-mouth", "105"}
		}
	case "redguard":
		if r.Sex == "female" {
			return []string{"-pitch", "40", "-speed", "72", "-throat", "112", "-mouth", "132"}
		} else {
			return []string{"-pitch", "64", "-speed", "72", "-throat", "118", "-mouth", "128"}
		}
	case "wood elf":
		if r.Sex == "female" {
			return []string{"-pitch", "40", "-speed", "74", "-throat", "100", "-mouth", "140"}
		} else {
			return []string{"-pitch", "60", "-speed", "74", "-throat", "100", "-mouth", "140"}
		}
	}

	return []string{"-pitch", "64", "-speed", "72", "-throat", "128", "-mouth", "128"}
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
				toMP3(tmpFile, outPath)
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
	cmd := exec.Command("sox", "-r", "12000", inFile, outPath)
	output, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("Command %q failed: %v\nOutput: %s\n", cmd.String(), err, output)
	}
	return nil
}
