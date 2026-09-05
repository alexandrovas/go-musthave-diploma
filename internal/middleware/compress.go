package middleware

import (
	"compress/gzip"
	"io"
	"net/http"
	"slices"
	"strings"
)

const (
	contentEncodingHeader  = "Content-Encoding"
	acceptEncodingHeader   = "Accept-Encoding"
	acceptEncodingGzip     = "gzip"
	contentTypeHeader      = "Content-Type"
	contentTypeApplicationJson = "application/json"
	contentTypeTextHtml        = "text/html"
)

// compressWriter реализует http.ResponseWriter и позволяет прозрачно
// сжимать передаваемые данные и выставлять правильные HTTP-заголовки
type compressWriter struct {
	w           http.ResponseWriter
	zw          *gzip.Writer
	compress    bool // выставляется в WriteHeader после проверки Content-Type
	wroteHeader bool
}

func newCompressWriter(w http.ResponseWriter) *compressWriter {
	return &compressWriter{
		w:  w,
		zw: gzip.NewWriter(w),
	}
}

func (c *compressWriter) Header() http.Header {
	return c.w.Header()
}

func (c *compressWriter) Write(p []byte) (int, error) {
	if !c.wroteHeader {
		c.WriteHeader(http.StatusOK)
	}
	if c.compress {
		return c.zw.Write(p)
	}
	return c.w.Write(p)
}

func (c *compressWriter) WriteHeader(statusCode int) {
	if c.wroteHeader {
		return
	}
	contentType := c.w.Header().Get(contentTypeHeader)
	if statusCode < 300 && allowToCompress(contentType) {
		c.w.Header().Set(contentEncodingHeader, acceptEncodingGzip)
		c.compress = true
	}
	c.w.WriteHeader(statusCode)
	c.wroteHeader = true
}

// Close закрывает gzip.Writer и досылает все данные из буфера
func (c *compressWriter) Close() error {
	if c.compress {
		return c.zw.Close()
	}
	return nil
}

// compressReader реализует io.ReadCloser и позволяет прозрачно
// декомпрессировать получаемые от клиента данные
type compressReader struct {
	r  io.ReadCloser
	zr *gzip.Reader
}

func newCompressReader(r io.ReadCloser) (*compressReader, error) {
	zr, err := gzip.NewReader(r)
	if err != nil {
		return nil, err
	}

	return &compressReader{
		r:  r,
		zr: zr,
	}, nil
}

func (c compressReader) Read(p []byte) (n int, err error) {
	return c.zr.Read(p)
}

func (c *compressReader) Close() error {
	if err := c.r.Close(); err != nil {
		return err
	}
	return c.zr.Close()
}

// allowToCompress проверяет, нужно ли сжимать ответ с данным Content-Type
func allowToCompress(contentType string) bool {
	allowed := []string{
		contentTypeApplicationJson,
		contentTypeTextHtml,
	}
	return slices.Contains(allowed, contentType)
}

// Compression — middleware для gzip-сжатия ответов и распаковки входящих запросов
func Compression(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		ow := w

		// Сжатие ответа: оборачиваем writer, когда клиент поддерживает gzip
		// Решение о фактическом сжатии принимается в WriteHeader на основе Content-Type
		acceptEncoding := r.Header.Get(acceptEncodingHeader)
		supportsGzip := strings.Contains(acceptEncoding, acceptEncodingGzip)
		if supportsGzip {
			cw := newCompressWriter(w)
			ow = cw
			defer cw.Close()
		}

		// Распаковка входящего запроса
		contentEncoding := r.Header.Get(contentEncodingHeader)
		sendsGzip := strings.Contains(contentEncoding, acceptEncodingGzip)
		if sendsGzip {
			cr, err := newCompressReader(r.Body)
			if err != nil {
				w.WriteHeader(http.StatusBadRequest)
				return
			}
			r.Body = cr
			defer cr.Close()
		}

		next.ServeHTTP(ow, r)
	})
}
