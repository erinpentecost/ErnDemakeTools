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
}

// envOverride sets a custom policy file for magick
var envOverride = []string{"MAGICK_CONFIGURE_PATH=."}

// getGroup returns a key that will group this file with other files that have the same key.
// If grouped, they are posterized together and will share the same pallete.
// If this returns an empty string, it is not processed by shrinker.
func getGroup(path string, info os.FileInfo) string {
	// skip menu files
	for _, block := range blocklist {
		if strings.Contains(strings.ToLower(info.Name()), block) {
			return ""
		}
	}

	// prefix determines which group the texture is posterized with.
	var prefix string
	if strings.HasPrefix(strings.ToLower(filepath.Base(info.Name())), "vfx_") {
		prefix = filepath.Base(info.Name())
	} else if strings.HasPrefix(strings.ToLower(filepath.Base(info.Name())), "tx_") && strings.Count(filepath.Base(info.Name()), "_") == 1 {
		prefix = filepath.Base(info.Name())
	} else if lastUnderscoreIndex := strings.LastIndex(info.Name(), "_"); lastUnderscoreIndex == -1 {
		// there is no underscore. strip off numbers from suffix
		prefix = strings.TrimSuffix(info.Name(), filepath.Ext(info.Name()))
		firstNum := strings.IndexAny(prefix, "0123456789")
		if firstNum != -1 {
			prefix = prefix[:firstNum]
		} else {
			prefix = info.Name()
		}
	} else if lastHairIndex := strings.LastIndex(info.Name(), "_hair"); lastHairIndex != -1 {
		// Hair should not be grouped with skin during posterization.
		//prefix = path[:len(path)-len(info.Name())+lastHairIndex] + "_hair"
		prefix = info.Name()
	} else {
		// Extract the prefix, which is the full path to the last underscore.
		prefix = path[:len(path)-len(info.Name())+lastUnderscoreIndex]
	}
	prefix = filepath.Base(prefix)

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

	var posterizedFileCount, segmentedFileCount atomic.Int64

	filesByPrefix := make(map[string][]string)
	err := filepath.Walk(rootDir, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		if info.IsDir() {
			return nil
		}
		if !strings.HasSuffix(strings.ToLower(info.Name()), ".dds") {
			return nil
		}
		if !slices.Contains(strings.Split(strings.ToLower(path), string(filepath.Separator)), "textures") {
			return nil
		}
		if g := getGroup(path, info); g != "" {
			filesByPrefix[g] = append(filesByPrefix[g], path)
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

	// processFile runs the remap pipeline on a single file.
	// Returns (segmented, error).
	processFile := func(f, colorMapFile, outputFilePath string) (bool, error) {
		args := []string{
			f,
			"(", "+clone", "-alpha", "extract", ")",
			"-alpha", "off",
			"-channel", "RGB",
			"-resize", "25%",
			"-filter", "Point",
			"-remap", colorMapFile,
			"+channel",
			"-compose", "CopyOpacity",
			"-composite",
		}
		if reduceValueContrast(outputFilePath) {
			args = append(args, "-brightness-contrast", "0x-50")
		}
		args = append(args, "-define", "dds:mipmaps=0", outputFilePath)

		if err := runProc("magick", args, envOverride); err != nil {
			return false, fmt.Errorf("remap failed for %q: %v", f, err)
		}

		if minColors <= 0 {
			return false, nil
		}

		// Count distinct colors in the output to decide if we need the kmeans fallback.
		colorCountOut, err := exec.Command(
			"magick", outputFilePath,
			"-channel", "RGB", "-alpha", "off",
			"-unique-colors", "-format", "%w", "info:",
		).Output()
		if err != nil {
			return false, fmt.Errorf("could not count colors for %q: %v", outputFilePath, err)
		}
		var distinctColors int
		if _, err := fmt.Sscanf(strings.TrimSpace(string(colorCountOut)), "%d", &distinctColors); err != nil {
			return false, fmt.Errorf("could not parse color count for %q: %v", outputFilePath, err)
		}
		if distinctColors >= minColors {
			return false, nil
		}

		// Too few colors — fall back to kmeans segmentation.
		fmt.Printf("Segmenting file: %s, only %d colors\n", f, distinctColors)
		args = []string{
			f,
			"(", "+clone", "-alpha", "extract", ")",
			"-alpha", "off",
			"-channel", "RGB",
			"-kmeans", "8",
			"+channel",
			"-compose", "CopyOpacity",
			"-composite",
		}
		if reduceValueContrast(outputFilePath) {
			args = append(args, "-brightness-contrast", "0x-50")
		}
		args = append(args, "-define", "dds:mipmaps=0", outputFilePath)

		if err := runProc("magick", args, envOverride); err != nil {
			return false, fmt.Errorf("kmeans failed for %q: %v", f, err)
		}
		return true, nil
	}

	group, ctx := errgroup.WithContext(ctx)
	group.SetLimit(workers)

	for prefix, files := range filesByPrefix {
		if len(files) == 0 {
			continue
		}
		group.Go(func() error {
			attempt := func() error {
				fmt.Printf("Processing prefix: %s\n", prefix)

				colorMapFile := filepath.Join(tempDir, fmt.Sprintf("PNG32:%s.png", prefix))
				defer os.Remove(colorMapFile)

				// First pass: shrink all files in the group into a buffer.
				shrinkArgs := append(append([]string{}, files...),
					"-resize", colorMapShrink, "-filter", "Point", "png:-",
				)
				buf := new(bytes.Buffer)
				shrinkCmd := exec.Command("magick", shrinkArgs...)
				shrinkCmd.Env = envOverride
				shrinkCmd.Stderr = os.Stderr
				shrinkCmd.Stdout = buf
				if err := shrinkCmd.Run(); err != nil {
					return fmt.Errorf("shrink failed for prefix %q: %v", prefix, err)
				}

				// Second pass: build a shared posterized color map.
				posterizeArgs := []string{
					"-", "-background", "none", "-append",
					"-channel", "RGB", "-alpha", "off",
					"-posterize", posterizeNormal,
					"-unique-colors", colorMapFile,
				}
				posterizeCmd := exec.Command("magick", posterizeArgs...)
				posterizeCmd.Env = envOverride
				posterizeCmd.Stdin = buf
				posterizeCmd.Stderr = os.Stderr
				if err := posterizeCmd.Run(); err != nil {
					return fmt.Errorf("posterize failed for prefix %q: %v", prefix, err)
				}

				// Third pass: remap each file individually, with one retry on failure.
				for _, f := range files {
					outputFilePath, err := repath(f)
					if err != nil {
						return fmt.Errorf("failed to get output path for %q: %v", f, err)
					}
					if err := os.MkdirAll(filepath.Dir(outputFilePath), os.ModePerm); err != nil {
						return fmt.Errorf("failed to make output dir for %q: %v", outputFilePath, err)
					}

					fmt.Printf("Processing file: %s\n", f)
					segmented, err := processFile(f, colorMapFile, outputFilePath)
					if err != nil {
						time.Sleep(time.Second)
						fmt.Printf("Retrying %q after error: %v\n", f, err)
						segmented, err = processFile(f, colorMapFile, outputFilePath)
						if err != nil {
							return err
						}
					}

					if segmented {
						segmentedFileCount.Add(1)
					} else {
						posterizedFileCount.Add(1)
					}
				}
				return nil
			}

			if err := attempt(); err != nil {
				fmt.Printf("Retrying prefix %q after error: %v\n", prefix, err)
				return attempt()
			}
			return nil
		})
	}

	if err := group.Wait(); err != nil {
		fmt.Println(err)
		fmt.Println("FAILURE!!")
		os.Exit(33)
	}
	fmt.Printf("Posterized files: %d, Segmented files: %d\n", posterizedFileCount.Load(), segmentedFileCount.Load())
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
	if err := os.Mkdir(outDir, os.ModePerm); err != nil {
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
