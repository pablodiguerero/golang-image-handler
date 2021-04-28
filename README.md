## About

Golang-based service to handling image "on the fly"

## Environment variables

| Variable name   | Decription                                 |
| --------------- | ------------------------------------------ |
| APP_PORT        | Port to attach application (default: 8001) |
| APP_IMAGE_ROOT  | Absolute path to images folder (required)  |
| APP_IMAGE_CACHE | Absolute path to images cache folder       |

## Supported url formats

- Template: **/images/\<basename>.fill-\<width>x\<height>.\<extension>**

  Example: **/images/awesomeimage.fill-200x200.jpg**

- Template: **/images/\<basename>.fit-\<width>x\<height>.\<extension>**

  Example: **/images/awesomeimage.fill-200x200.jpg**

- Template: **/images/\<basename>.fitstrict-\<width>x\<height>.\<extension>**

  Example: **/images/awesomeimage.fitstrict-200x200.jpg**
