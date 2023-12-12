package main

import (
	"fmt"
	"io"
	"net/http"
	"os"
	"strconv"
	"sync"

	"github.com/labstack/echo/v4"
	"github.com/labstack/echo/v4/middleware"
)

type FileInfo struct {
	init             bool
	file             *os.File
	uploaded         []bool
	uploadedChunkNum int64
}

type UploadServer struct {
	fileInfo map[string]FileInfo
	lock     sync.RWMutex
}

func (s *UploadServer) TestChunk(c echo.Context) error {
	s.lock.RLock()
	defer s.lock.RUnlock()

	identifier := c.QueryParam("identifier")
	chunkNumber, err := strconv.ParseInt(c.QueryParam("chunkNumber"), 10, 64)
	if err != nil {
		return err
	}
	fileInfo, ok := s.fileInfo[identifier]
	if !ok {
		return c.NoContent(http.StatusNoContent)
	}

	if !fileInfo.init {
		return c.NoContent(http.StatusNoContent)
	}

	// 这里返回的应该得是 permanentErrors，不是 404. 不然前端会上传失败而不是重传块。
	// 梁哥应该得改一下。
	if !fileInfo.uploaded[chunkNumber-1] {
		return c.NoContent(http.StatusNoContent)
	}

	return c.NoContent(http.StatusOK)
}

func (s *UploadServer) UploadFile(c echo.Context) error {
	s.lock.Lock()
	defer s.lock.Unlock()

	path := "uploads"

	// handle the request
	chunkNumber, err := strconv.ParseInt(c.FormValue("chunkNumber"), 10, 64)
	if err != nil {
		return c.JSON(http.StatusBadRequest, err)
	}
	chunkSize, err := strconv.ParseInt(c.FormValue("chunkSize"), 10, 64)
	if err != nil {
		return c.JSON(http.StatusBadRequest, err)
	}
	currentChunkSize, err := strconv.ParseInt(c.FormValue("currentChunkSize"), 10, 64)
	if err != nil {
		return c.JSON(http.StatusBadRequest, err)
	}
	totalChunks, err := strconv.ParseInt(c.FormValue("totalChunks"), 10, 64)
	if err != nil {
		return c.JSON(http.StatusBadRequest, err)
	}
	totalSize, err := strconv.ParseInt(c.FormValue("totalSize"), 10, 64)
	if err != nil {
		return c.JSON(http.StatusBadRequest, err)
	}

	identifier := c.FormValue("identifier")
	fileName := c.FormValue("filename")
	bin, err := c.FormFile("file")
	if err != nil {
		return c.JSON(http.StatusBadRequest, err)
	}

	// file info handle
	fileInfo, ok := s.fileInfo[identifier]
	if !ok {
		file, err := os.Create(path + "/" + fileName + ".tmp")
		if err != nil {
			return c.JSON(http.StatusInternalServerError, err)
		}

		// pre allocate file size
		err = file.Truncate(totalSize)
		if err != nil {
			return c.JSON(http.StatusInternalServerError, err)
		}
		s.fileInfo[identifier] = FileInfo{
			file:             file,
			init:             true,
			uploaded:         make([]bool, totalChunks),
			uploadedChunkNum: 0,
		}
		fileInfo = s.fileInfo[identifier]
	}

	if err != nil {
		return c.JSON(http.StatusInternalServerError, err)
	}

	_, err = fileInfo.file.Seek((chunkNumber-1)*chunkSize, io.SeekStart)
	if err != nil {
		return c.JSON(http.StatusInternalServerError, err)
	}

	src, err := bin.Open()
	if err != nil {
		return c.JSON(http.StatusInternalServerError, err)
	}
	defer src.Close()

	buf := make([]byte, int(currentChunkSize))
	// TODO zerocopy
	_, err = io.CopyBuffer(fileInfo.file, src, buf)
	if err != nil {
		fmt.Println(err)
		return c.JSON(http.StatusInternalServerError, err)
	}

	// handle file after write a chunk
	fileInfo.uploaded[chunkNumber-1] = true
	s.fileInfo[identifier] = FileInfo{
		file:     fileInfo.file,
		init:     true,
		uploaded: fileInfo.uploaded,
		// TODO handle single chunk upload twice
		uploadedChunkNum: fileInfo.uploadedChunkNum + 1,
	}

	// handle file after write all chunk
	if fileInfo.uploadedChunkNum == totalChunks-1 {
		fileInfo.file.Close()
		os.Rename(path+"/"+fileName+".tmp", path+"/"+fileName)
	}
	return c.NoContent(http.StatusOK)
}

func main() {
	e := echo.New()
	// e.Use(middleware.Logger())

	e.Use(middleware.CORSWithConfig(middleware.CORSConfig{
		AllowOrigins: []string{"*"},
		AllowHeaders: []string{echo.HeaderOrigin, echo.HeaderContentType, echo.HeaderAccept},
		AllowMethods: []string{http.MethodGet, http.MethodPost, http.MethodPut, http.MethodDelete},
	}))

	s := &UploadServer{
		fileInfo: make(map[string]FileInfo),
		lock:     sync.RWMutex{},
	}
	e.GET("/upload", s.TestChunk)
	e.POST("/upload", s.UploadFile)
	e.OPTIONS("/upload", func(c echo.Context) error {
		return c.NoContent(http.StatusOK)
	})
	e.Logger.Fatal(e.Start(":3000"))
}
