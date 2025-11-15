<!-- omit from toc -->
# `>_` clitools

A collection of simple CLI tools which I use ocasionally.  
They're really simple, mostly single-file, and should theoretically "just work" across platforms.

- [`(imageconvert)` Mass Image Converter using ImageMagick](#imageconvert-mass-image-converter-using-imagemagick)
- [`(mediaconvert)` Mass Media Converter using FFMPEG](#mediaconvert-mass-media-converter-using-ffmpeg)
- [`(crunchy)` Turn Image into a Crunchy JPEG](#crunchy-turn-image-into-a-crunchy-jpeg)

<br>

## `(imageconvert)` Mass Image Converter using ImageMagick 
Ever have a directory full of images in different formats? Use this tool to
quickly convert them to a normal extension. Outputs to a `convert` folder
in the current working directory. 

> **Requires:** ImageMagick

```
imageconvert
    --skip-errors   - Skip on conversion error
    --skip-resume   - Skip Resume Checking
    --multithread   - Use Multiple Threads
    --recursive     - Scan Directories Recursively
    <From>          - File Extension(s) to convert from, delimited with comma
    <To>            - File Extension to convert into
    [Arguments]     - Arguments to pass onto ImageMagick
```

<br>

## `(mediaconvert)` Mass Media Converter using FFMPEG 
Ever have a directory full of videos in different formats? Use this tool to
quickly convert them to a normal extension. Outputs to a `convert` folder
in the current working directory. 

> **Requires:** FFMPEG

```
mediaconvert
    --skip-resume   - Skip Resume Checking
    --multithread	 - Use Multiple Threads
    --recursive     - Scan Directories Recursively
    <From>          - File Extension(s) to convert from, delimited with comma
    <To>            - File Extension to convert into
    [Arguments]     - Arguments to pass onto FFMPEG
```

<br>

## `(crunchy)` Turn Image into a Crunchy JPEG
Applies random noise and rounding errors to the colorspace to make an image look **"crunchy"**

```
crunchy
	--noise=<value>       - Noise Level  (Default: 25, Range: 0-100)
	--quality=<value>	   - JPEG Quality (Default: 0,  Range: 0-100)
    --generations=<count> - Iterations   (Default: 5)
    <Filename>            - Input Filename
```

<p align="center">
    <img src="crunchy/example.png">
    <p align="center">Another satisfied customer!</p>
</p>

<br>
