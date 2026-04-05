package search

import (
	"fmt"
	"strings"
)

const systemPromptTemplate = `You are a knowledgeable assistant with access to a curated knowledge base.
Answer the user's question using only the provided context. If the context doesn't contain enough information to answer, say so clearly.
Cite your sources using the qi:// URIs provided with each context passage.
Format citations inline like: [qi://collection/path].`

// BuildPrompt constructs a RAG prompt from search results.
func BuildPrompt(question string, results []Result) (systemPrompt, userPrompt string) {
	systemPrompt = systemPromptTemplate

	var ctx strings.Builder
	ctx.WriteString("## Context\n\n")

	for i, r := range results {
		uri := fmt.Sprintf("qi://%s/%s", r.Collection, r.Path)
		heading := ""
		if r.HeadingPath != "" {
			heading = " (" + r.HeadingPath + ")"
		}
		ctx.WriteString(fmt.Sprintf("### [%d] %s%s\n", i+1, uri, heading))
		ctx.WriteString(r.Snippet)
		ctx.WriteString("\n\n")
	}

	ctx.WriteString("## Question\n\n")
	ctx.WriteString(question)

	userPrompt = ctx.String()
	return systemPrompt, userPrompt
}
