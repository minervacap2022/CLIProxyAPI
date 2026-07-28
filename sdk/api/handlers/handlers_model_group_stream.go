package handlers

import (
	"context"
	"net/http"

	internalconfig "github.com/router-for-me/CLIProxyAPI/v7/internal/config"
	"github.com/router-for-me/CLIProxyAPI/v7/internal/interfaces"
)

func (h *BaseAPIHandler) executeStreamWithModelGroup(ctx context.Context, entryProtocol, exitProtocol string, group *internalconfig.ModelGroup, rawJSON []byte, alt string, allowImageModel bool, execOptions modelExecutionOptions) (<-chan []byte, http.Header, <-chan *interfaces.ErrorMessage) {
	dataOut := make(chan []byte)
	errOut := make(chan *interfaces.ErrorMessage, 1)
	var selectedHeaders http.Header
	var lastErr *interfaces.ErrorMessage

	for _, candidate := range modelGroupCandidates(group) {
		candidateOptions := execOptions
		candidateOptions.ModelGroupResolved = true
		data, headers, errs := h.executeStreamWithAuthManagerFormats(ctx, entryProtocol, exitProtocol, candidate, rawJSON, alt, allowImageModel, candidateOptions)
		var first []byte
		var streamErr *interfaces.ErrorMessage
		for first == nil && streamErr == nil && (data != nil || errs != nil) {
			select {
			case <-ctx.Done():
				return dataOut, nil, errOut
			case chunk, ok := <-data:
				if !ok {
					data = nil
					continue
				}
				if len(chunk) > 0 {
					first = chunk
				}
			case errMsg, ok := <-errs:
				if !ok {
					errs = nil
					continue
				}
				streamErr = errMsg
			}
		}
		if streamErr != nil && shouldModelGroupFailover(streamErr.Error) {
			lastErr = streamErr
			selectedHeaders = headers
			continue
		}
		selectedHeaders = headers

		go func(data <-chan []byte, errs <-chan *interfaces.ErrorMessage, first []byte, initialErr *interfaces.ErrorMessage) {
			defer close(dataOut)
			defer close(errOut)
			if initialErr != nil {
				errOut <- initialErr
				return
			}
			if first == nil {
				return
			}
			select {
			case <-ctx.Done():
				return
			case dataOut <- first:
			}
			for data != nil || errs != nil {
				select {
				case <-ctx.Done():
					return
				case chunk, ok := <-data:
					if !ok {
						data = nil
						continue
					}
					dataOut <- chunk
				case errMsg, ok := <-errs:
					if !ok {
						errs = nil
						continue
					}
					errOut <- errMsg
					return
				}
			}
		}(data, errs, first, streamErr)
		return dataOut, selectedHeaders, errOut
	}
	close(dataOut)
	if lastErr != nil {
		errOut <- lastErr
	}
	close(errOut)
	return dataOut, selectedHeaders, errOut
}
