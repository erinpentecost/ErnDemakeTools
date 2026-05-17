// This is a trashy script you run with "go run shrinker.go /folder/to/shrink".
// You must run it from this dir.
// Get kram from https://github.com/bmdhacks/kram
package main

import (
	"bytes"
	"context"
	"fmt"
	"log"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"slices"
	"strings"
	"sync/atomic"
	"syscall"
	"time"

	"golang.org/x/sync/errgroup"
)

const (
	workers         = 12
	colorMapShrink  = "256x256>"
	posterizeNormal = "3"
	minColors       = 2
	finalShrink     = "25%"

	kramBlockSize = "8x8"
	kramQuality   = "80"
)

var blocklist = []string{
	"menu_",
	"_menu",
	"_cursor",
	"cursor_",
	"compass.dds",
	"scroll.dds",
	"target.dds",
	"mapfowtexture.dds",
	"detect_animal_icon.dds",
	"detect_enchantment_icon.dds",
	"detect_key_icon.dds",
	"door_icon.dds",
	"tx_leveluptoken.dds",
	"tx_scroll_bar.dds",
	"tx_scroll_button.dds",
	// this causes seizures during ash storms
	"tx_ash_flake.dds",
	"tx_ash_cloud.dds",
	// don't mess with specular, normal, and normal heightmap textures
	"_spec.dds",
	//"_n.dds", // there are false positives here
	"_nh.dds",
	"tx_dwrv_blackbelt00", // pure black texture messes with other dwarven stuff
	//"tx_c_ring_", // these are size 0?
	"tx_sun_flash_grey_05.dds", // crazy sun glare
	"td_detect_enemy_icon.dds",
	"td_detect_humanoid_icon.dds",
	"td_detect_invisibility_icon.dds",
}

// envOverride sets a custom policy file for magick
var envOverride = []string{"MAGICK_CONFIGURE_PATH=."}

// getGroup returns a key that will group this file with other files that have the same key.
// If grouped, they are posterized together and will share the same pallete.
// If this returns an empty string, it is not processed by shrinker.
func getGroup(path string) string {
	fileName := strings.ToLower(filepath.Base(path))

	// skip menu files
	for _, block := range blocklist {
		if strings.Contains(fileName, block) {
			return ""
		}
	}

	fileName = strings.TrimPrefix(strings.TrimSuffix(fileName, filepath.Ext(fileName)), "tx_")

	// prefix determines which group the texture is posterized with.

	var prefix string
	if strings.HasPrefix(fileName, "vfx_") {
		prefix = fileName
	} else if lastUnderscoreIndex := strings.LastIndex(fileName, "_"); lastUnderscoreIndex == -1 {
		// there is no underscore. strip off numbers from suffix
		prefix = fileName
		firstNum := strings.IndexAny(prefix, "0123456789")
		if firstNum != -1 {
			prefix = prefix[:firstNum]
		} else {
			prefix = fileName
		}
	} else if lastHairIndex := strings.LastIndex(fileName, "_hair"); lastHairIndex != -1 {
		// Hair should not be grouped with skin during posterization.
		//prefix = path[:len(path)-len(info.Name())+lastHairIndex] + "_hair"
		prefix = fileName
	} else {
		// Extract the prefix, which is the full path to the last underscore.
		prefix = fileName[:lastUnderscoreIndex]
	}

	return prefix
}

func reduceValueContrast(path string) bool {
	normalized := strings.ToLower(filepath.Base(path))
	return strings.HasPrefix(normalized, "water")
}

func kram(ctx context.Context, rootDir string, outDir string) {
	//kram_cmd = ['kram', 'encode', '-f', f'astc{args.block_size}', '-encoder', 'astcenc', '-quality', args.quality, '-flip', '-i', str(thread_temp_png), '-o', str(final_ktx_path)]
	repath := func(f string) (string, error) {
		absFile, err := filepath.Abs(f)
		if err != nil {
			return "", err
		}
		newPath := filepath.Join(outDir, absFile[len(rootDir):])

		return strings.TrimSuffix(newPath, filepath.Ext(newPath)) + ".ktx", nil
	}

	var counter atomic.Int64

	tempDir, err := os.MkdirTemp(os.TempDir(), "kram-")
	if err != nil {
		log.Fatalf("Error walking the making temp dir: %v", err)
	}
	defer os.RemoveAll(tempDir)

	group, ctx := errgroup.WithContext(ctx)
	group.SetLimit(workers)

	err = filepath.Walk(rootDir, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}

		// Skip directories.
		if info.IsDir() {
			return nil
		}

		// Process only files with the ".dds" extension.
		if !strings.HasSuffix(strings.ToLower(info.Name()), ".dds") {
			return nil
		}

		group.Go(func() error {
			// compress the file. kram expects a png.
			id := counter.Add(1)
			pngPath := filepath.Join(tempDir, fmt.Sprintf("%d.png", id))
			args := []string{
				path,
				pngPath,
			}
			if err := runProc("magick", args, envOverride); err != nil {
				return fmt.Errorf("Failed to process file %q: %v", path, err)
			}
			defer os.Remove(pngPath)

			outPath, err := repath(path)
			if err != nil {
				return fmt.Errorf("Failed to find new path for %q: %v", path, err)
			}
			// make sure directory is there
			if err := os.MkdirAll(filepath.Dir(outPath), os.ModePerm); err != nil {
				return fmt.Errorf("Failed to make output dir for %q: %v", outPath, err)
			}

			// now that we have a png, run kram.
			kramArgs := []string{
				"encode",
				"-f",
				"astc" + kramBlockSize,
				"-encoder",
				"astcenc",
				"-quality",
				kramQuality,
				"-flip",
				"-i",
				pngPath,
				"-o",
				outPath,
			}
			if err := runProc("/home/ern/bin/kram", kramArgs, []string{}); err != nil {
				return fmt.Errorf("Failed to process file %q: %v", path, err)
			}
			return nil
		})
		return nil
	})

	if err != nil {
		log.Fatalf("Error walking the directory: %v", err)
	}
}

func shrink(ctx context.Context, rootDir string, outDir string) {
	repath := func(f string) (string, error) {
		absFile, err := filepath.Abs(f)
		if err != nil {
			return "", err
		}
		return filepath.Join(outDir, absFile[len(rootDir):]), nil
	}

	posterizedFileCount := atomic.Int64{}
	segmentedFileCount := atomic.Int64{}

	filesByPrefix := make(map[string][]string)
	err := filepath.Walk(rootDir, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		if info.IsDir() ||
			!strings.HasSuffix(strings.ToLower(info.Name()), ".dds") ||
			!slices.Contains(strings.Split(strings.ToLower(path), string(filepath.Separator)), "textures") {
			return nil
		}
		if group := getGroup(path); group != "" {
			filesByPrefix[group] = append(filesByPrefix[group], path)
		}
		return nil
	})
	if err != nil {
		log.Fatalf("Error walking the directory: %v", err)
	}

	tempDir, err := os.MkdirTemp(os.TempDir(), "shrink-")
	if err != nil {
		log.Fatalf("Error making temp dir: %v", err)
	}
	defer os.RemoveAll(tempDir)

	eg, ctx := errgroup.WithContext(ctx)
	eg.SetLimit(workers)

	for prefix, files := range filesByPrefix {
		if len(files) == 0 {
			continue
		}
		eg.Go(func() error {
			fmt.Printf("Processing prefix: %s\n", prefix)

			colorMapFile := filepath.Join(tempDir, fmt.Sprintf("PNG32:%s.png", prefix))

			// Pass 1: shrink all files in the group into a buffer.
			buf := new(bytes.Buffer)
			shrinkCmd := exec.Command("magick", append(files,
				"-resize", colorMapShrink, "-filter", "Point", "png:-")...)
			shrinkCmd.Env = envOverride
			shrinkCmd.Stderr = os.Stderr
			shrinkCmd.Stdout = buf
			if err := shrinkCmd.Run(); err != nil {
				return fmt.Errorf("failed to shrink prefix %q: %w", prefix, err)
			}

			// Pass 2: posterize the buffer to build a shared color map.
			posterizeCmd := exec.Command("magick",
				"-", "-background", "none", "-append",
				"-channel", "RGB", "-alpha", "off",
				"-posterize", posterizeNormal, "-unique-colors", colorMapFile)
			posterizeCmd.Env = envOverride
			posterizeCmd.Stdin = buf
			posterizeCmd.Stderr = os.Stderr
			if err := posterizeCmd.Run(); err != nil {
				return fmt.Errorf("failed to posterize prefix %q: %w", prefix, err)
			}
			defer os.Remove(colorMapFile)

			// Pass 3: remap + resize each file individually.
			for _, f := range files {
				if err := processFile(f, colorMapFile, repath, &posterizedFileCount, &segmentedFileCount); err != nil {
					// Retry once on failure.
					fmt.Printf("Retrying file %q after error: %v\n", f, err)
					time.Sleep(time.Second)
					if err := processFile(f, colorMapFile, repath, &posterizedFileCount, &segmentedFileCount); err != nil {
						return fmt.Errorf("failed to process file %q after retry: %w", f, err)
					}
				}
			}
			return nil
		})
	}

	if err := eg.Wait(); err != nil {
		fmt.Println(err)
		fmt.Println("FAILURE!!")
		os.Exit(33)
	}
	fmt.Printf("Groups: %d, Posterized files: %d, Segmented files: %d\n", len(filesByPrefix), posterizedFileCount.Load(), segmentedFileCount.Load())
}

func processFile(
	f, colorMapFile string,
	repath func(string) (string, error),
	posterizedFileCount, segmentedFileCount *atomic.Int64,
) error {
	outputFilePath, err := repath(f)
	if err != nil {
		return fmt.Errorf("failed to get output path: %w", err)
	}
	if err := os.MkdirAll(filepath.Dir(outputFilePath), os.ModePerm); err != nil {
		return fmt.Errorf("failed to make output dir: %w", err)
	}
	if _, err := os.Stat(outputFilePath); err == nil {
		fmt.Printf("Skipping file: %s\n", f)
		return nil
	}
	fmt.Printf("Processing file: %s\n", f)

	// Extract alpha channel to a temp file.
	alphaFile, err := os.CreateTemp("", "alpha-*.png")
	if err != nil {
		return fmt.Errorf("failed to create alpha temp file: %w", err)
	}
	alphaPath := alphaFile.Name()
	alphaFile.Close()
	defer os.Remove(alphaPath)

	if err := runProc("magick", []string{
		f, "-alpha", "extract", alphaPath,
	}, envOverride); err != nil {
		return fmt.Errorf("failed to extract alpha: %w", err)
	}

	// Remap colors (no alpha involvement).
	args := []string{
		f,
		"-alpha", "off",
		"-channel", "RGB",
		"-remap", colorMapFile,
		"+channel",
		"-resize", "25%", "-filter", "Point",
	}
	if reduceValueContrast(outputFilePath) {
		args = append(args, "-brightness-contrast", "0x-50")
	}
	args = append(args, "-define", "dds:mipmaps=0", outputFilePath)
	if err := runProc("magick", args, envOverride); err != nil {
		return fmt.Errorf("failed to remap: %w", err)
	}

	// Apply saved alpha back onto the output.
	if err := applyAlpha(outputFilePath, alphaPath); err != nil {
		return fmt.Errorf("failed to apply alpha: %w", err)
	}

	if minColors <= 0 {
		posterizedFileCount.Add(1)
		return nil
	}

	colorCountOut, err := exec.Command("magick", outputFilePath,
		"-channel", "RGB", "-alpha", "off",
		"-unique-colors", "-format", "%w", "info:").Output()
	var distinctColors int
	needsSegmentation := err != nil
	if err == nil {
		if _, scanErr := fmt.Sscanf(strings.TrimSpace(string(colorCountOut)), "%d", &distinctColors); scanErr != nil {
			fmt.Printf("Warning: could not parse color count for %q: %v\n", outputFilePath, scanErr)
			needsSegmentation = true
		} else {
			needsSegmentation = distinctColors < minColors
		}
	} else {
		fmt.Printf("Warning: could not count colors for %q: %v\n", outputFilePath, err)
	}

	if !needsSegmentation {
		posterizedFileCount.Add(1)
		return nil
	}

	segmentedFileCount.Add(1)
	fmt.Printf("Segmenting file: %s, only %d colors\n", f, distinctColors)

	args = []string{
		f,
		"-alpha", "off",
		"-channel", "RGB", "-kmeans", "6",
		"+channel",
		"-resize", "25%", "-filter", "Point",
	}
	if reduceValueContrast(outputFilePath) {
		args = append(args, "-brightness-contrast", "0x-50")
	}
	args = append(args, "-define", "dds:mipmaps=0", outputFilePath)
	if err := runProc("magick", args, envOverride); err != nil {
		return fmt.Errorf("failed to segment: %w", err)
	}

	if err := applyAlpha(outputFilePath, alphaPath); err != nil {
		return fmt.Errorf("failed to apply alpha after segmentation: %w", err)
	}

	posterizedFileCount.Add(1)
	return nil
}

func applyAlpha(imagePath, alphaPath string) error {
	return runProc("magick", []string{
		imagePath,
		alphaPath,
		"-compose", "CopyOpacity", "-composite",
		imagePath,
	}, envOverride)
}

func main() {
	// Check for a single command-line argument (the directory path).
	if len(os.Args) != 4 {
		fmt.Printf("Usage: %s <shrink|kram> <input_directory_path> <output_directory_path>\n", os.Args[0])
		os.Exit(1)
	}

	rootDir, err := filepath.Abs(os.Args[2])
	if err != nil {
		log.Fatalf("Error: The input path %q is messed up: %v", os.Args[2], err)
	}
	// Verify that the provided path exists and is a directory.
	info, err := os.Stat(rootDir)
	if os.IsNotExist(err) || !info.IsDir() {
		log.Fatalf("Error: The input path %q does not exist or is not a directory.", rootDir)
	}

	outDir, err := filepath.Abs(os.Args[3])
	if err != nil {
		log.Fatalf("Error: The output path %q is messed up: %v", os.Args[3], err)
	}

	if err := os.MkdirAll(outDir, os.ModePerm); err != nil {
		log.Fatalf("Error: Failed to make %q: %v", outDir, err)
	}

	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt, os.Kill, syscall.SIGTERM)
	defer cancel()

	switch os.Args[1] {
	case "shrink":
		shrink(ctx, rootDir, outDir)
	case "kram":
		kram(ctx, rootDir, outDir)
	default:
		log.Fatalf("Error: Bad argument %q", os.Args[1])
	}
}

func runProc(exe string, args []string, env []string) error {
	cmd := exec.Command(exe, args...)
	if len(env) != 0 {
		cmd.Env = env
	}

	fmt.Printf("Executing command: %s\n", cmd.String())

	// Set the working directory to the directory of the first file to simplify paths.
	//cmd.Dir = filepath.Dir(args[0])

	// Capture the combined stdout and stderr.
	output, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("Command %q failed: %v\nOutput: %s\n", cmd.String(), err, output)
	}
	return nil
}
