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

	"golang.org/x/sync/errgroup"
)

const (
	workers         = 12
	colorMapShrink  = "256x256>"
	posterizeNormal = "3"
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

	// filesByPrefix is a map to store the grouped file paths.
	// The key is the shared prefix, and the value is a slice of full file paths.
	filesByPrefix := make(map[string][]string)

	// Walk the directory and populate the map.
	err := filepath.Walk(rootDir, func(path string, info os.FileInfo, err error) error {
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
		// only deal with texture files
		if !slices.Contains(strings.Split(strings.ToLower(path), string(filepath.Separator)), "textures") {
			return nil
		}
		group := getGroup(path, info)
		if group == "" {
			return nil
		}
		filesByPrefix[group] = append(filesByPrefix[group], path)
		return nil
	})

	if err != nil {
		log.Fatalf("Error walking the directory: %v", err)
	}

	tempDir, err := os.MkdirTemp(os.TempDir(), "shrink-")
	if err != nil {
		log.Fatalf("Error walking the making temp dir: %v", err)
	}
	defer os.RemoveAll(tempDir)

	group, ctx := errgroup.WithContext(ctx)
	group.SetLimit(workers)

	// Iterate over the map and process each group of files.
	for prefix, files := range filesByPrefix {
		if len(files) == 0 {
			continue
		}
		group.Go(func() error {
			fmt.Printf("Processing prefix: %s\n", prefix)

			// Create the output filename using the shared prefix.
			colorMapFile := filepath.Join(tempDir, fmt.Sprintf("%s.png", prefix))

			// In the first pass, we shrink all the members of the prefix group
			// and place the results in an in-memory buffer.
			shrinkArgs := []string{}
			shrinkArgs = append(shrinkArgs, files...)
			shrinkArgs = append(shrinkArgs,
				"-resize",
				colorMapShrink,
				"-filter",
				"Point",
				"png:-",
			)

			buf := new(bytes.Buffer)

			shrinkCmd := exec.Command("magick", shrinkArgs...)
			shrinkCmd.Env = envOverride
			shrinkCmd.Stderr = os.Stderr
			shrinkCmd.Stdout = buf
			if err := shrinkCmd.Run(); err != nil {
				return fmt.Errorf("Failed to shrink prefix '%s': %v", prefix, err)
			}

			// In this next step, we posterize all the members of the prefix
			// in order to build up a shared color map for the entire prefix.
			posterizeArgs := []string{
				"-",
				"-background", "none",
				"-append",
				"-channel",
				"RGB",
				"-alpha",
				"off",
				"-posterize",
				posterizeNormal,
				"-unique-colors",
				colorMapFile,
			}

			posterizeCmd := exec.Command("magick", posterizeArgs...)
			posterizeCmd.Env = envOverride
			posterizeCmd.Stdin = buf
			posterizeCmd.Stderr = os.Stderr
			if err := posterizeCmd.Run(); err != nil {
				return fmt.Errorf("Failed to posterize prefix %q: %v", prefix, err)
			}
			defer os.Remove(colorMapFile)

			// Finally, we rescale + recolor the individual files.
			for _, f := range files {
				outputFilePath, err := repath(f)
				if err != nil {
					return fmt.Errorf("Failed to get output path for %q: %v", f, err)
				}
				// make sure directory is there
				if err := os.MkdirAll(filepath.Dir(outputFilePath), os.ModePerm); err != nil {
					return fmt.Errorf("Failed to make output dir for %q: %v", outputFilePath, err)
				}

				// Posterize!
				fmt.Printf("Processing file: %s\n", f)
				var args []string
				args = []string{
					f,
					"(", "+clone", "-alpha", "extract", ")",
					"-alpha", "off",
					"-channel", "RGB",
					"-remap", colorMapFile,
					"+channel",
					"-resize", "25%",
					"-filter", "Point",
					"-compose", "CopyOpacity",
					"-composite",
				}

				if reduceValueContrast(outputFilePath) {
					args = append(args, "-brightness-contrast", "0x-50")
				}

				// Don't bother with mipmaps. The textures are already small.
				args = append(args, "-define", "dds:mipmaps=0")

				args = append(args, outputFilePath)

				if err := runProc("magick", args, envOverride); err != nil {
					return fmt.Errorf("Failed to process file %q: %v", f, err)
				}

				// Analyze the output image's colors. If there are fewer than 3
				// distinct colors, fall back to a kmeans segmentation strategy
				// (the remap approach produces banding/cursed results on simple textures).
				colorCountOut, colorCountErr := exec.Command(
					"magick", outputFilePath,
					"-channel", "RGB",
					"-alpha", "off",
					"-unique-colors",
					"-format", "%w",
					"info:",
				).Output()
				notEnoughDistinctColors := false
				if colorCountErr != nil {
					// If we can't count colors, assume we need the fallback.
					fmt.Printf("Warning: could not count colors for %q: %v\n", outputFilePath, colorCountErr)
					notEnoughDistinctColors = true
				} else {
					var distinctColors int
					if _, err := fmt.Sscanf(strings.TrimSpace(string(colorCountOut)), "%d", &distinctColors); err != nil {
						fmt.Printf("Warning: could not parse color count for %q: %v\n", outputFilePath, err)
						notEnoughDistinctColors = true
					} else {
						notEnoughDistinctColors = distinctColors < 3
						fmt.Printf("Distinct colors in %q: %d\n", outputFilePath, distinctColors)
					}
				}

				if notEnoughDistinctColors {
					fmt.Printf("Segmenting file: %s\n", f)
					// segment first using kmeans to reduce noise before
					// posterizing. this makes faces less cursed.
					args = []string{
						f,
						"(", "+clone", "-alpha", "extract", ")",
						"-alpha", "off",
						"-channel", "RGB",
						"-kmeans", "27",
						"+channel",
						"-resize", "25%",
						"-filter", "Point",
						"-compose", "CopyOpacity",
						"-composite",
					}
					if reduceValueContrast(outputFilePath) {
						args = append(args, "-brightness-contrast", "0x-50")
					}

					// Don't bother with mipmaps. The textures are already small.
					args = append(args, "-define", "dds:mipmaps=0")

					args = append(args, outputFilePath)

					if err := runProc("magick", args, envOverride); err != nil {
						return fmt.Errorf("Failed to process file %q: %v", f, err)
					}
				}

			}
			return nil
		})
	}

	if err := group.Wait(); err != nil {
		fmt.Println(err)
		fmt.Println("FAILURE!!")
		os.Exit(33)
	}
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
