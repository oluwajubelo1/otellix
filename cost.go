package otellix

import (
	"sync"

	"github.com/oluwajubelo1/otellix/providers"
)

// PricingEntry holds the per-million-token pricing for a specific model.
type PricingEntry struct {
	InputPricePerMToken      float64 // USD per 1 million input tokens
	OutputPricePerMToken     float64 // USD per 1 million output tokens
	CacheReadPricePerMToken  float64 // USD per 1 million cached tokens read (hits)
	CacheWritePricePerMToken float64 // USD per 1 million cached tokens written (creation)
}

// pricingTable holds the default pricing keyed by "provider/model".
// Protected by pricingMu for concurrent RegisterModel calls.
// Prices are USD per 1 million tokens (July–August 2026).
var (
	pricingMu    sync.RWMutex
	pricingTable = map[string]PricingEntry{
		// --- Anthropic ---
		"anthropic/claude-opus-4-5":            {InputPricePerMToken: 5.0, OutputPricePerMToken: 25.0, CacheReadPricePerMToken: 0.50, CacheWritePricePerMToken: 6.25},
		"anthropic/claude-opus-4-6":            {InputPricePerMToken: 5.0, OutputPricePerMToken: 25.0, CacheReadPricePerMToken: 0.50, CacheWritePricePerMToken: 6.25},
		"anthropic/claude-opus-4-7":            {InputPricePerMToken: 5.0, OutputPricePerMToken: 25.0, CacheReadPricePerMToken: 0.50, CacheWritePricePerMToken: 6.25},
		"anthropic/claude-opus-4-8":            {InputPricePerMToken: 5.0, OutputPricePerMToken: 25.0, CacheReadPricePerMToken: 0.50, CacheWritePricePerMToken: 6.25},
		"anthropic/claude-opus-5":              {InputPricePerMToken: 5.0, OutputPricePerMToken: 25.0, CacheReadPricePerMToken: 0.50, CacheWritePricePerMToken: 6.25},
		"anthropic/claude-opus-4-1":            {InputPricePerMToken: 15.0, OutputPricePerMToken: 75.0, CacheReadPricePerMToken: 1.50, CacheWritePricePerMToken: 18.75},
		"anthropic/claude-opus-4":              {InputPricePerMToken: 15.0, OutputPricePerMToken: 75.0, CacheReadPricePerMToken: 1.50, CacheWritePricePerMToken: 18.75},
		"anthropic/claude-3-opus":              {InputPricePerMToken: 15.0, OutputPricePerMToken: 75.0, CacheReadPricePerMToken: 1.50, CacheWritePricePerMToken: 18.75},
		"anthropic/claude-3-opus-20240229":     {InputPricePerMToken: 15.0, OutputPricePerMToken: 75.0, CacheReadPricePerMToken: 1.50, CacheWritePricePerMToken: 18.75},
		"anthropic/claude-sonnet-4-5":          {InputPricePerMToken: 3.0, OutputPricePerMToken: 15.0, CacheReadPricePerMToken: 0.30, CacheWritePricePerMToken: 3.75},
		"anthropic/claude-sonnet-4-6":          {InputPricePerMToken: 3.0, OutputPricePerMToken: 15.0, CacheReadPricePerMToken: 0.30, CacheWritePricePerMToken: 3.75},
		"anthropic/claude-sonnet-5":            {InputPricePerMToken: 3.0, OutputPricePerMToken: 15.0, CacheReadPricePerMToken: 0.30, CacheWritePricePerMToken: 3.75},
		"anthropic/claude-3-5-sonnet":          {InputPricePerMToken: 3.0, OutputPricePerMToken: 15.0, CacheReadPricePerMToken: 0.30, CacheWritePricePerMToken: 3.75},
		"anthropic/claude-3-5-sonnet-20240620": {InputPricePerMToken: 3.0, OutputPricePerMToken: 15.0, CacheReadPricePerMToken: 0.30, CacheWritePricePerMToken: 3.75},
		"anthropic/claude-3-7-sonnet":          {InputPricePerMToken: 3.0, OutputPricePerMToken: 15.0, CacheReadPricePerMToken: 0.30, CacheWritePricePerMToken: 3.75},
		"anthropic/claude-sonnet-4":            {InputPricePerMToken: 3.0, OutputPricePerMToken: 15.0, CacheReadPricePerMToken: 0.30, CacheWritePricePerMToken: 3.75},
		"anthropic/claude-haiku-4-5":           {InputPricePerMToken: 0.80, OutputPricePerMToken: 4.0, CacheReadPricePerMToken: 0.08, CacheWritePricePerMToken: 1.00},
		"anthropic/claude-haiku-3-5":           {InputPricePerMToken: 0.80, OutputPricePerMToken: 4.0, CacheReadPricePerMToken: 0.08, CacheWritePricePerMToken: 1.00},
		"anthropic/claude-3-5-haiku":           {InputPricePerMToken: 0.80, OutputPricePerMToken: 4.0, CacheReadPricePerMToken: 0.08, CacheWritePricePerMToken: 1.00},
		"anthropic/claude-haiku-3":             {InputPricePerMToken: 0.25, OutputPricePerMToken: 1.25, CacheReadPricePerMToken: 0.025, CacheWritePricePerMToken: 0.3125},
		"anthropic/claude-3-haiku":             {InputPricePerMToken: 0.25, OutputPricePerMToken: 1.25, CacheReadPricePerMToken: 0.025, CacheWritePricePerMToken: 0.3125},
		"anthropic/claude-fable-5":             {InputPricePerMToken: 10.0, OutputPricePerMToken: 50.0, CacheReadPricePerMToken: 1.00, CacheWritePricePerMToken: 12.50},
		"anthropic/claude-mythos-5":            {InputPricePerMToken: 10.0, OutputPricePerMToken: 50.0, CacheReadPricePerMToken: 1.00, CacheWritePricePerMToken: 12.50},

		// --- OpenAI ---
		"openai/gpt-5.5":       {InputPricePerMToken: 5.0, OutputPricePerMToken: 30.0, CacheReadPricePerMToken: 0.50, CacheWritePricePerMToken: 5.0},
		"openai/gpt-5.5-pro":   {InputPricePerMToken: 30.0, OutputPricePerMToken: 180.0, CacheReadPricePerMToken: 3.0, CacheWritePricePerMToken: 30.0},
		"openai/gpt-5.4":       {InputPricePerMToken: 2.50, OutputPricePerMToken: 15.0, CacheReadPricePerMToken: 0.25, CacheWritePricePerMToken: 2.50},
		"openai/gpt-5.4-mini":  {InputPricePerMToken: 0.30, OutputPricePerMToken: 1.50, CacheReadPricePerMToken: 0.03, CacheWritePricePerMToken: 0.30},
		"openai/gpt-5.4-nano":  {InputPricePerMToken: 0.10, OutputPricePerMToken: 0.50, CacheReadPricePerMToken: 0.01, CacheWritePricePerMToken: 0.10},
		"openai/gpt-5":         {InputPricePerMToken: 1.25, OutputPricePerMToken: 10.0, CacheReadPricePerMToken: 0.125, CacheWritePricePerMToken: 1.25},
		"openai/gpt-5-mini":    {InputPricePerMToken: 0.25, OutputPricePerMToken: 2.0, CacheReadPricePerMToken: 0.025, CacheWritePricePerMToken: 0.25},
		"openai/gpt-5-nano":    {InputPricePerMToken: 0.05, OutputPricePerMToken: 0.40, CacheReadPricePerMToken: 0.005, CacheWritePricePerMToken: 0.05},
		"openai/gpt-4o":        {InputPricePerMToken: 2.50, OutputPricePerMToken: 10.0, CacheReadPricePerMToken: 1.25, CacheWritePricePerMToken: 2.50},
		"openai/gpt-4o-mini":   {InputPricePerMToken: 0.15, OutputPricePerMToken: 0.60, CacheReadPricePerMToken: 0.075, CacheWritePricePerMToken: 0.15},
		"openai/gpt-4.1":       {InputPricePerMToken: 2.0, OutputPricePerMToken: 8.0, CacheReadPricePerMToken: 0.20, CacheWritePricePerMToken: 2.0},
		"openai/gpt-4.1-mini":  {InputPricePerMToken: 0.40, OutputPricePerMToken: 1.60, CacheReadPricePerMToken: 0.04, CacheWritePricePerMToken: 0.40},
		"openai/gpt-4.1-nano":  {InputPricePerMToken: 0.10, OutputPricePerMToken: 0.40, CacheReadPricePerMToken: 0.01, CacheWritePricePerMToken: 0.10},
		"openai/gpt-4-turbo":   {InputPricePerMToken: 10.0, OutputPricePerMToken: 30.0, CacheReadPricePerMToken: 1.0, CacheWritePricePerMToken: 10.0},
		"openai/gpt-4":         {InputPricePerMToken: 30.0, OutputPricePerMToken: 60.0, CacheReadPricePerMToken: 3.0, CacheWritePricePerMToken: 30.0},
		"openai/gpt-3.5-turbo": {InputPricePerMToken: 0.50, OutputPricePerMToken: 1.50, CacheReadPricePerMToken: 0.05, CacheWritePricePerMToken: 0.50},
		"openai/o3":            {InputPricePerMToken: 2.0, OutputPricePerMToken: 8.0, CacheReadPricePerMToken: 0.20, CacheWritePricePerMToken: 2.0},
		"openai/o3-mini":       {InputPricePerMToken: 1.10, OutputPricePerMToken: 4.40, CacheReadPricePerMToken: 0.11, CacheWritePricePerMToken: 1.10},
		"openai/o3-pro":        {InputPricePerMToken: 20.0, OutputPricePerMToken: 80.0, CacheReadPricePerMToken: 2.0, CacheWritePricePerMToken: 20.0},
		"openai/o4-mini":       {InputPricePerMToken: 1.10, OutputPricePerMToken: 4.40, CacheReadPricePerMToken: 0.11, CacheWritePricePerMToken: 1.10},
		"openai/o1":            {InputPricePerMToken: 15.0, OutputPricePerMToken: 60.0, CacheReadPricePerMToken: 1.5, CacheWritePricePerMToken: 15.0},
		"openai/o1-mini":       {InputPricePerMToken: 3.0, OutputPricePerMToken: 12.0, CacheReadPricePerMToken: 0.3, CacheWritePricePerMToken: 3.0},
		"openai/o1-pro":        {InputPricePerMToken: 150.0, OutputPricePerMToken: 600.0, CacheReadPricePerMToken: 15.0, CacheWritePricePerMToken: 150.0},
		"openai/o1-preview":    {InputPricePerMToken: 15.0, OutputPricePerMToken: 60.0, CacheReadPricePerMToken: 1.5, CacheWritePricePerMToken: 15.0},
		"openai/gpt-oss-120b":  {InputPricePerMToken: 0.30, OutputPricePerMToken: 1.20, CacheReadPricePerMToken: 0.03, CacheWritePricePerMToken: 0.30},
		"openai/gpt-oss-20b":   {InputPricePerMToken: 0.03, OutputPricePerMToken: 0.15, CacheReadPricePerMToken: 0.003, CacheWritePricePerMToken: 0.03},

		// --- Google Gemini ---
		"gemini/gemini-3.6-flash":      {InputPricePerMToken: 1.50, OutputPricePerMToken: 7.50, CacheReadPricePerMToken: 0.375, CacheWritePricePerMToken: 1.50},
		"gemini/gemini-3.5-flash":      {InputPricePerMToken: 1.50, OutputPricePerMToken: 9.0, CacheReadPricePerMToken: 0.375, CacheWritePricePerMToken: 1.50},
		"gemini/gemini-3.5-flash-lite": {InputPricePerMToken: 0.30, OutputPricePerMToken: 2.50, CacheReadPricePerMToken: 0.075, CacheWritePricePerMToken: 0.30},
		"gemini/gemini-3.1-pro":        {InputPricePerMToken: 2.0, OutputPricePerMToken: 12.0, CacheReadPricePerMToken: 0.50, CacheWritePricePerMToken: 2.0},
		"gemini/gemini-3-pro":          {InputPricePerMToken: 2.0, OutputPricePerMToken: 12.0, CacheReadPricePerMToken: 0.50, CacheWritePricePerMToken: 2.0},
		"gemini/gemini-3-flash":        {InputPricePerMToken: 0.50, OutputPricePerMToken: 3.0, CacheReadPricePerMToken: 0.125, CacheWritePricePerMToken: 0.50},
		"gemini/gemini-2.5-pro":        {InputPricePerMToken: 1.25, OutputPricePerMToken: 10.0, CacheReadPricePerMToken: 0.31, CacheWritePricePerMToken: 1.25},
		"gemini/gemini-2.5-flash":      {InputPricePerMToken: 0.30, OutputPricePerMToken: 2.50, CacheReadPricePerMToken: 0.075, CacheWritePricePerMToken: 0.30},
		"gemini/gemini-2.5-flash-lite": {InputPricePerMToken: 0.10, OutputPricePerMToken: 0.40, CacheReadPricePerMToken: 0.025, CacheWritePricePerMToken: 0.10},
		"gemini/gemini-2.0-flash":      {InputPricePerMToken: 0.10, OutputPricePerMToken: 0.40, CacheReadPricePerMToken: 0.025, CacheWritePricePerMToken: 0.10},
		"gemini/gemini-2.0-flash-lite": {InputPricePerMToken: 0.075, OutputPricePerMToken: 0.30, CacheReadPricePerMToken: 0.018, CacheWritePricePerMToken: 0.075},
		"gemini/gemini-1.5-pro":        {InputPricePerMToken: 1.25, OutputPricePerMToken: 5.0, CacheReadPricePerMToken: 0.31, CacheWritePricePerMToken: 1.25},
		"gemini/gemini-1.5-flash":      {InputPricePerMToken: 0.075, OutputPricePerMToken: 0.30, CacheReadPricePerMToken: 0.018, CacheWritePricePerMToken: 0.075},

		// --- Moonshot AI (Kimi) ---
		"kimi/kimi-k3":                {InputPricePerMToken: 3.0, OutputPricePerMToken: 15.0, CacheReadPricePerMToken: 0.30, CacheWritePricePerMToken: 3.0},
		"kimi/kimi-k2.7-code":         {InputPricePerMToken: 0.95, OutputPricePerMToken: 4.0, CacheReadPricePerMToken: 0.19, CacheWritePricePerMToken: 0.95},
		"kimi/kimi-k2.6":              {InputPricePerMToken: 0.95, OutputPricePerMToken: 4.0, CacheReadPricePerMToken: 0.16, CacheWritePricePerMToken: 0.95},
		"kimi/kimi-k2.5":              {InputPricePerMToken: 0.60, OutputPricePerMToken: 3.0, CacheReadPricePerMToken: 0.10, CacheWritePricePerMToken: 0.60},
		"kimi/kimi-k2":                {InputPricePerMToken: 0.60, OutputPricePerMToken: 2.50, CacheReadPricePerMToken: 0.10, CacheWritePricePerMToken: 0.60},
		"kimi/kimi-k2-0905-preview":   {InputPricePerMToken: 0.60, OutputPricePerMToken: 2.50, CacheReadPricePerMToken: 0.10, CacheWritePricePerMToken: 0.60},
		"kimi/kimi-k2-0711-preview":   {InputPricePerMToken: 0.60, OutputPricePerMToken: 2.50, CacheReadPricePerMToken: 0.10, CacheWritePricePerMToken: 0.60},
		"kimi/kimi-k2-thinking":       {InputPricePerMToken: 0.60, OutputPricePerMToken: 2.50, CacheReadPricePerMToken: 0.10, CacheWritePricePerMToken: 0.60},
		"kimi/kimi-k2-turbo-preview":  {InputPricePerMToken: 1.15, OutputPricePerMToken: 8.0, CacheReadPricePerMToken: 0.15, CacheWritePricePerMToken: 1.15},
		"kimi/kimi-k2-thinking-turbo": {InputPricePerMToken: 1.15, OutputPricePerMToken: 8.0, CacheReadPricePerMToken: 0.15, CacheWritePricePerMToken: 1.15},
		"kimi/moonshot-v1-8k":         {InputPricePerMToken: 0.20, OutputPricePerMToken: 2.0, CacheReadPricePerMToken: 0.0, CacheWritePricePerMToken: 0.0},
		"kimi/moonshot-v1-32k":        {InputPricePerMToken: 1.0, OutputPricePerMToken: 3.0, CacheReadPricePerMToken: 0.0, CacheWritePricePerMToken: 0.0},
		"kimi/moonshot-v1-128k":       {InputPricePerMToken: 2.0, OutputPricePerMToken: 5.0, CacheReadPricePerMToken: 0.0, CacheWritePricePerMToken: 0.0},

		// --- DeepSeek ---
		"deepseek/deepseek-v4":       {InputPricePerMToken: 0.30, OutputPricePerMToken: 0.50, CacheReadPricePerMToken: 0.03, CacheWritePricePerMToken: 0.30},
		"deepseek/deepseek-v4-flash": {InputPricePerMToken: 0.14, OutputPricePerMToken: 0.28, CacheReadPricePerMToken: 0.014, CacheWritePricePerMToken: 0.14},
		"deepseek/deepseek-v4-pro":   {InputPricePerMToken: 0.435, OutputPricePerMToken: 0.87, CacheReadPricePerMToken: 0.0435, CacheWritePricePerMToken: 0.435},
		"deepseek/deepseek-r1":       {InputPricePerMToken: 0.55, OutputPricePerMToken: 2.19, CacheReadPricePerMToken: 0.14, CacheWritePricePerMToken: 0.55},
		"deepseek/deepseek-reasoner": {InputPricePerMToken: 0.55, OutputPricePerMToken: 2.19, CacheReadPricePerMToken: 0.14, CacheWritePricePerMToken: 0.55},
		"deepseek/deepseek-chat":     {InputPricePerMToken: 0.28, OutputPricePerMToken: 0.42, CacheReadPricePerMToken: 0.028, CacheWritePricePerMToken: 0.28},
		"deepseek/deepseek-v3":       {InputPricePerMToken: 0.27, OutputPricePerMToken: 1.10, CacheReadPricePerMToken: 0.027, CacheWritePricePerMToken: 0.27},
		"deepseek/deepseek-v3.1":     {InputPricePerMToken: 0.27, OutputPricePerMToken: 0.95, CacheReadPricePerMToken: 0.027, CacheWritePricePerMToken: 0.27},
		"deepseek/deepseek-v3.2":     {InputPricePerMToken: 0.28, OutputPricePerMToken: 0.42, CacheReadPricePerMToken: 0.028, CacheWritePricePerMToken: 0.28},

		// --- Alibaba Qwen ---
		"qwen/qwen3.5-397b":  {InputPricePerMToken: 0.60, OutputPricePerMToken: 3.60, CacheReadPricePerMToken: 0.0, CacheWritePricePerMToken: 0.0},
		"qwen/qwen3.5-plus":  {InputPricePerMToken: 0.40, OutputPricePerMToken: 2.40, CacheReadPricePerMToken: 0.0, CacheWritePricePerMToken: 0.0},
		"qwen/qwen3.5-flash": {InputPricePerMToken: 0.10, OutputPricePerMToken: 0.40, CacheReadPricePerMToken: 0.0, CacheWritePricePerMToken: 0.0},
		"qwen/qwen3.7-flash": {InputPricePerMToken: 0.03, OutputPricePerMToken: 0.13, CacheReadPricePerMToken: 0.0, CacheWritePricePerMToken: 0.0},
		"qwen/qwen-max":      {InputPricePerMToken: 1.20, OutputPricePerMToken: 3.60, CacheReadPricePerMToken: 0.0, CacheWritePricePerMToken: 0.0},
		"qwen/qwen-plus":     {InputPricePerMToken: 0.40, OutputPricePerMToken: 1.20, CacheReadPricePerMToken: 0.0, CacheWritePricePerMToken: 0.0},
		"qwen/qwen-turbo":    {InputPricePerMToken: 0.20, OutputPricePerMToken: 0.60, CacheReadPricePerMToken: 0.0, CacheWritePricePerMToken: 0.0},

		// --- Zhipu GLM ---
		"glm/glm-5.2":       {InputPricePerMToken: 1.40, OutputPricePerMToken: 4.40, CacheReadPricePerMToken: 0.28, CacheWritePricePerMToken: 1.40},
		"glm/glm-5.1":       {InputPricePerMToken: 1.40, OutputPricePerMToken: 4.40, CacheReadPricePerMToken: 0.28, CacheWritePricePerMToken: 1.40},
		"glm/glm-5":         {InputPricePerMToken: 1.0, OutputPricePerMToken: 3.20, CacheReadPricePerMToken: 0.20, CacheWritePricePerMToken: 1.0},
		"glm/glm-4.7":       {InputPricePerMToken: 0.60, OutputPricePerMToken: 2.20, CacheReadPricePerMToken: 0.11, CacheWritePricePerMToken: 0.60},
		"glm/glm-4.6":       {InputPricePerMToken: 0.60, OutputPricePerMToken: 2.20, CacheReadPricePerMToken: 0.11, CacheWritePricePerMToken: 0.60},
		"glm/glm-4.5":       {InputPricePerMToken: 0.60, OutputPricePerMToken: 2.20, CacheReadPricePerMToken: 0.11, CacheWritePricePerMToken: 0.60},
		"glm/glm-4.5-air":   {InputPricePerMToken: 0.20, OutputPricePerMToken: 1.10, CacheReadPricePerMToken: 0.03, CacheWritePricePerMToken: 0.20},
		"glm/glm-4.5-flash": {InputPricePerMToken: 0.0, OutputPricePerMToken: 0.0, CacheReadPricePerMToken: 0.0, CacheWritePricePerMToken: 0.0},
		"glm/glm-4.7-flash": {InputPricePerMToken: 0.0, OutputPricePerMToken: 0.0, CacheReadPricePerMToken: 0.0, CacheWritePricePerMToken: 0.0},

		// --- Baidu ERNIE ---
		"ernie/ernie-4.5":       {InputPricePerMToken: 0.55, OutputPricePerMToken: 2.20, CacheReadPricePerMToken: 0.0, CacheWritePricePerMToken: 0.0},
		"ernie/ernie-x1":        {InputPricePerMToken: 0.28, OutputPricePerMToken: 1.10, CacheReadPricePerMToken: 0.0, CacheWritePricePerMToken: 0.0},
		"ernie/ernie-4.5-turbo": {InputPricePerMToken: 0.07, OutputPricePerMToken: 0.28, CacheReadPricePerMToken: 0.0, CacheWritePricePerMToken: 0.0},
		"ernie/ernie-speed":     {InputPricePerMToken: 0.017, OutputPricePerMToken: 0.017, CacheReadPricePerMToken: 0.0, CacheWritePricePerMToken: 0.0},

		// --- Tencent Hunyuan ---
		"hunyuan/hunyuan-hy3":      {InputPricePerMToken: 0.063, OutputPricePerMToken: 0.21, CacheReadPricePerMToken: 0.021, CacheWritePricePerMToken: 0.063},
		"hunyuan/hunyuan-large":    {InputPricePerMToken: 0.56, OutputPricePerMToken: 1.68, CacheReadPricePerMToken: 0.0, CacheWritePricePerMToken: 0.0},
		"hunyuan/hunyuan-standard": {InputPricePerMToken: 0.07, OutputPricePerMToken: 0.14, CacheReadPricePerMToken: 0.0, CacheWritePricePerMToken: 0.0},

		// --- Mistral ---
		"mistral/mistral-medium-3.5": {InputPricePerMToken: 1.50, OutputPricePerMToken: 7.50, CacheReadPricePerMToken: 0.0, CacheWritePricePerMToken: 0.0},
		"mistral/mistral-large":      {InputPricePerMToken: 2.0, OutputPricePerMToken: 6.0, CacheReadPricePerMToken: 0.0, CacheWritePricePerMToken: 0.0},
		"mistral/mistral-large-3":    {InputPricePerMToken: 0.50, OutputPricePerMToken: 1.50, CacheReadPricePerMToken: 0.0, CacheWritePricePerMToken: 0.0},
		"mistral/mistral-small":      {InputPricePerMToken: 0.20, OutputPricePerMToken: 0.60, CacheReadPricePerMToken: 0.0, CacheWritePricePerMToken: 0.0},
		"mistral/mistral-small-4":    {InputPricePerMToken: 0.10, OutputPricePerMToken: 0.30, CacheReadPricePerMToken: 0.0, CacheWritePricePerMToken: 0.0},
		"mistral/codestral":          {InputPricePerMToken: 0.30, OutputPricePerMToken: 0.90, CacheReadPricePerMToken: 0.0, CacheWritePricePerMToken: 0.0},
		"mistral/mistral-nemo":       {InputPricePerMToken: 0.15, OutputPricePerMToken: 0.15, CacheReadPricePerMToken: 0.0, CacheWritePricePerMToken: 0.0},
		"mistral/ministral-3b":       {InputPricePerMToken: 0.04, OutputPricePerMToken: 0.04, CacheReadPricePerMToken: 0.0, CacheWritePricePerMToken: 0.0},
		"mistral/ministral-8b":       {InputPricePerMToken: 0.10, OutputPricePerMToken: 0.10, CacheReadPricePerMToken: 0.0, CacheWritePricePerMToken: 0.0},
		"mistral/magistral-medium":   {InputPricePerMToken: 2.0, OutputPricePerMToken: 5.0, CacheReadPricePerMToken: 0.0, CacheWritePricePerMToken: 0.0},
		"mistral/magistral-small":    {InputPricePerMToken: 0.50, OutputPricePerMToken: 1.50, CacheReadPricePerMToken: 0.0, CacheWritePricePerMToken: 0.0},

		// --- xAI Grok ---
		"grok/grok-4":      {InputPricePerMToken: 3.0, OutputPricePerMToken: 15.0, CacheReadPricePerMToken: 0.0, CacheWritePricePerMToken: 0.0},
		"grok/grok-4.3":    {InputPricePerMToken: 1.25, OutputPricePerMToken: 2.50, CacheReadPricePerMToken: 0.0, CacheWritePricePerMToken: 0.0},
		"grok/grok-4.20":   {InputPricePerMToken: 2.0, OutputPricePerMToken: 6.0, CacheReadPricePerMToken: 0.0, CacheWritePricePerMToken: 0.0},
		"grok/grok-3":      {InputPricePerMToken: 3.0, OutputPricePerMToken: 15.0, CacheReadPricePerMToken: 0.0, CacheWritePricePerMToken: 0.0},
		"grok/grok-4-fast": {InputPricePerMToken: 0.20, OutputPricePerMToken: 0.50, CacheReadPricePerMToken: 0.0, CacheWritePricePerMToken: 0.0},

		// --- Ollama (local, free) ---
		"ollama/any": {InputPricePerMToken: 0, OutputPricePerMToken: 0, CacheReadPricePerMToken: 0, CacheWritePricePerMToken: 0},

		// --- Embedding models ---
		"openai/text-embedding-3-small":  {InputPricePerMToken: 0.02, OutputPricePerMToken: 0, CacheReadPricePerMToken: 0, CacheWritePricePerMToken: 0},
		"openai/text-embedding-3-large":  {InputPricePerMToken: 0.13, OutputPricePerMToken: 0, CacheReadPricePerMToken: 0, CacheWritePricePerMToken: 0},
		"openai/text-embedding-ada-002":  {InputPricePerMToken: 0.10, OutputPricePerMToken: 0, CacheReadPricePerMToken: 0, CacheWritePricePerMToken: 0},
		"gemini/text-embedding-004":      {InputPricePerMToken: 0.10, OutputPricePerMToken: 0, CacheReadPricePerMToken: 0, CacheWritePricePerMToken: 0},
		"cohere/embed-english-v3.0":      {InputPricePerMToken: 0.10, OutputPricePerMToken: 0, CacheReadPricePerMToken: 0, CacheWritePricePerMToken: 0},
		"cohere/embed-multilingual-v3.0": {InputPricePerMToken: 0.10, OutputPricePerMToken: 0, CacheReadPricePerMToken: 0, CacheWritePricePerMToken: 0},
	}
)

// CalculateCost computes the USD cost for an LLM call based on the provider,
// model, and token counts in the result.
//
// Returns 0 for unknown provider/model combinations (does not panic).
func CalculateCost(provider, model string, result providers.CallResult) float64 {
	entry, ok := GetPricing(provider, model)
	if !ok {
		// Check for wildcard ollama pricing.
		if provider == "ollama" {
			return 0
		}
		return 0
	}

	inputCost := float64(result.InputTokens) * entry.InputPricePerMToken / 1_000_000
	outputCost := float64(result.OutputTokens) * entry.OutputPricePerMToken / 1_000_000
	readCacheCost := float64(result.CacheReadTokens) * entry.CacheReadPricePerMToken / 1_000_000
	writeCacheCost := float64(result.CacheWriteTokens) * entry.CacheWritePricePerMToken / 1_000_000

	return inputCost + outputCost + readCacheCost + writeCacheCost
}

// EstimateCost estimates the cost assuming the given number of output tokens.
// Used for pre-call budget checks where actual token counts are unknown.
func EstimateCost(provider, model string, estimatedOutputTokens int64) float64 {
	entry, ok := GetPricing(provider, model)
	if !ok {
		if provider == "ollama" {
			return 0
		}
		return 0
	}
	// Rough estimate: assume input tokens ≈ output tokens for estimation.
	return float64(estimatedOutputTokens) * (entry.InputPricePerMToken + entry.OutputPricePerMToken) / 1_000_000
}

// CalculateEmbeddingCost computes the USD cost for an embedding call, which is
// billed purely on input tokens. Returns 0 for unknown provider/model pairs.
func CalculateEmbeddingCost(provider, model string, inputTokens int64) float64 {
	entry, ok := GetPricing(provider, model)
	if !ok {
		return 0
	}
	return float64(inputTokens) * entry.InputPricePerMToken / 1_000_000
}

// GetPricing returns the pricing entry for a specific provider/model combination.
// Returns (entry, false) if not found.
func GetPricing(provider, model string) (PricingEntry, bool) {
	key := provider + "/" + model
	pricingMu.RLock()
	defer pricingMu.RUnlock()

	entry, ok := pricingTable[key]
	if !ok {
		// Try ollama wildcard.
		if provider == "ollama" {
			entry, ok = pricingTable["ollama/any"]
		}
	}
	return entry, ok
}

// RegisterModel adds or updates pricing for a provider/model combination.
// This allows callers to add custom models or override built-in pricing.
//
// Example:
//
//	otellix.RegisterModel("anthropic", "claude-4-opus", otellix.PricingEntry{
//	    InputPricePerMToken:  20.0,
//	    OutputPricePerMToken: 100.0,
//	})
func RegisterModel(provider, model string, entry PricingEntry) {
	key := provider + "/" + model
	pricingMu.Lock()
	defer pricingMu.Unlock()
	pricingTable[key] = entry
}

// ListPricing returns a snapshot of all known provider/model pricing entries.
// The returned map's keys are in "provider/model" format (e.g., "anthropic/claude-sonnet-4-6").
// This is useful for analyzing pricing data and suggesting model alternatives.
func ListPricing() map[string]PricingEntry {
	pricingMu.RLock()
	defer pricingMu.RUnlock()

	out := make(map[string]PricingEntry, len(pricingTable))
	for k, v := range pricingTable {
		out[k] = v
	}
	return out
}
