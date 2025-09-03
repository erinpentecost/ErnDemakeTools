# Building

You'll use `tes3cmd` to dump records from .esm files into text files.

For normal dialogues, the command looks like this:

    tes3cmd dump --type INFO --match "SNAM:" '/home/ern/tes3/mods/ModdingResources/TamrielData/00 Data Files/Tamriel_Data.esm' | iconv -t UTF-8//IGNORE | tee /home/ern/workspace/speechtotext/tamriel_data.txt

This output can be read directly by `go run parse.go`.

For voices played inside scripts, you will need to do manual cleanup and create a CSV. This type of record can be extracted like this:

    tes3cmd dump  '/home/ern/tes3/Morrowind/Data Files/Bloodmoon.esm' | iconv -t UTF-8//IGNORE | grep -i "say" >> /home/ern/workspace/speechtotext/bloodmoon_says.txt

The first column is the sound file. The second is the subtitle. The third should be sex, if it exists, and the fourth should be race, if it exists. Creatures probably don't have either.

Then you create the replacement dialogue like this:

go run  "./parse.go" "/home/ern/tes3/mods/Audio/DemakeAudio/New Audio Files" "./dumps/Morrowind.txt,./dumps/Bloodmoon.txt,./dumps/Tribunal.txt,./dumps/big_says.csv"

## Double checking

`diff --ignore-file-name-case -r '/home/ern/tes3/Morrowind/Data Files/Sound/Vo/' '/home/ern/tes3/mods/Audio/DemakeAudio/00 Vanilla/Sound/vo/' | grep "Only in '/home/ern/tes3/Morrowind/Data"`

## Dependencies

`tes3cmd` from:
- https://modding-openmw.com/mods/momw-tools-pack/
- https://modding-openmw.gitlab.io/momw-tools-pack/
- https://gitlab.com/modding-openmw/momw-tools-pack/-/package_files/212976856/download

`SAM`. You can disable SDL support. Get it from:
- https://github.com/erinpentecost/SAM

`sox`, from your package manager.

# TODO

create a dummy folder with all voiceovers replaced by silence.mp3. this will be loaded first.
