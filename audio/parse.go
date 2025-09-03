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

func altFile(path string) ([]Record, error) {
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
		records = append(records, Record{
			SoundFile: csvRecords[0][0],
			Response:  csvRecords[0][1],
		})
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
	switch r.Race {
	case "argonian":
		if r.Sex == "female" {
			return []string{"-pitch", "48", "-speed", "66", "-throat", "60", "-mouth", "132"}
		} else {
			return []string{"-pitch", "32", "-speed", "60", "-throat", "64", "-mouth", "128"}
		}
	case "breton":
		if r.Sex == "female" {
			return []string{"-pitch", "56", "-speed", "76", "-throat", "120", "-mouth", "132"}
		} else {
			return []string{"-pitch", "40", "-speed", "72", "-throat", "128", "-mouth", "128"}
		}
	case "dark elf":
		if r.Sex == "female" {
			return []string{"-pitch", "48", "-speed", "70", "-throat", "92", "-mouth", "116"}
		} else {
			return []string{"-pitch", "36", "-speed", "68", "-throat", "96", "-mouth", "110"}
		}
	case "high elf":
		if r.Sex == "female" {
			return []string{"-pitch", "60", "-speed", "78", "-throat", "136", "-mouth", "128"}
		} else {
			return []string{"-pitch", "44", "-speed", "74", "-throat", "140", "-mouth", "124"}
		}
	case "imperial":
		if r.Sex == "female" {
			return []string{"-pitch", "54", "-speed", "74", "-throat", "124", "-mouth", "124"}
		} else {
			return []string{"-pitch", "42", "-speed", "70", "-throat", "128", "-mouth", "120"}
		}
	case "khajiit":
		if r.Sex == "female" {
			return []string{"-pitch", "50", "-speed", "68", "-throat", "108", "-mouth", "144"}
		} else {
			return []string{"-pitch", "38", "-speed", "64", "-throat", "112", "-mouth", "140"}
		}
	case "nord":
		if r.Sex == "female" {
			return []string{"-pitch", "46", "-speed", "70", "-throat", "144", "-mouth", "122"}
		} else {
			return []string{"-pitch", "34", "-speed", "66", "-throat", "150", "-mouth", "118"}
		}
	case "orc":
		if r.Sex == "female" {
			return []string{"-pitch", "40", "-speed", "66", "-throat", "156", "-mouth", "114"}
		} else {
			return []string{"-pitch", "30", "-speed", "62", "-throat", "160", "-mouth", "110"}
		}
	case "redguard":
		if r.Sex == "female" {
			return []string{"-pitch", "58", "-speed", "80", "-throat", "112", "-mouth", "132"}
		} else {
			return []string{"-pitch", "42", "-speed", "76", "-throat", "118", "-mouth", "128"}
		}
	case "wood elf":
		if r.Sex == "female" {
			return []string{"-pitch", "62", "-speed", "82", "-throat", "96", "-mouth", "140"}
		} else {
			return []string{"-pitch", "46", "-speed", "78", "-throat", "100", "-mouth", "136"}
		}
	}

	return []string{"-pitch", "44", "-speed", "72", "-throat", "128", "-mouth", "128"}
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

	dedupe := map[string]interface{}{}

	var records []Record
	for _, f := range strings.Split(os.Args[2], ",") {
		var frecords []Record
		var err error
		if filepath.Ext(f) == ".csv" {
			frecords, err = altFile(f)
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
	//group, ctx := errgroup.WithContext(ctx)
	//group.SetLimit(12)

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
			// it can stay a wav file if the original was a wav.
			switch filepath.Ext(strings.ToLower(r.SoundFile)) {
			case ".wav":
				fmt.Printf("Moving %q to %q\n", tmpFile, outPath)
				if err := MoveFile(tmpFile, outPath); err != nil {
					return err
				}
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
	/*err = group.Wait()
	if err != nil {
		fmt.Printf("ERROR!!!! %v", err)
	}*/
}

func MoveFile(sourcePath, destPath string) error {
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

	/*if err := outputFile.Sync(); err != nil {
	return fmt.Errorf("sync failed: %w", err)
	}*/

	/*if err := os.Remove(sourcePath); err != nil {
	return fmt.Errorf("failed removing original file: %w", err)
	}*/
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

func toWAV(inFile string, outPath string) error {
	cmd := exec.Command("sox", "-r", "12000", inFile, outPath)
	output, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("Command %q failed: %v\nOutput: %s\n", cmd.String(), err, output)
	}
	return nil
}
