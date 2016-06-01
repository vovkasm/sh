// Copyright (c) 2016, Daniel Martí <mvdan@mvdan.cc>
// See LICENSE for licensing information

package sh

import (
	"fmt"
	"io"
	"strings"
)

func Fprint(w io.Writer, n Node) error {
	p := printer{
		w: w,
	}
	if f, ok := n.(File); ok {
		p.comments = f.Comments
	}
	p.node(n)
	return p.err
}

type printer struct {
	w   io.Writer
	err error

	wantSpace bool

	splitNotEscaped bool

	curLine int
	level   int

	comments []Comment

	stack []Node
}

func (p *printer) nestedBinary() bool {
	if len(p.stack) < 3 {
		return false
	}
	_, ok := p.stack[len(p.stack)-3].(BinaryExpr)
	return ok
}

func (p *printer) inBinary() bool {
	for i := len(p.stack) - 1; i >= 0; i-- {
		switch p.stack[i].(type) {
		case BinaryExpr:
			return true
		case Stmt:
			return false
		}
	}
	return false
}

func (p *printer) inArithm() bool {
	for i := len(p.stack) - 1; i >= 0; i-- {
		switch p.stack[i].(type) {
		case ArithmExpr, LetStmt, CStyleCond, CStyleLoop:
			return true
		case Stmt:
			return false
		}
	}
	return false
}

func (p *printer) compactArithm() bool {
	for i := len(p.stack) - 1; i >= 0; i-- {
		switch p.stack[i].(type) {
		case LetStmt:
			return true
		case ParenExpr:
			return false
		}
	}
	return false
}

var (
	// these never want a following space
	contiguousRight = map[Token]bool{
		DOLLPR:  true,
		LPAREN:  true,
		DLPAREN: true,
		BQUOTE:  true,
		CMDIN:   true,
		DOLLDP:  true,
	}
	// these never want a preceding space
	contiguousLeft = map[Token]bool{
		SEMICOLON:  true,
		DSEMICOLON: true,
		RPAREN:     true,
		DRPAREN:    true,
		COMMA:      true,
		BQUOTE:     true,
	}
)

func (p *printer) space(b byte) {
	if p.err != nil {
		return
	}
	_, p.err = p.w.Write([]byte{b})
	p.wantSpace = false
}

func (p *printer) nonSpaced(a ...interface{}) {
	for _, v := range a {
		if p.err != nil {
			break
		}
		switch x := v.(type) {
		case string:
			if len(x) > 0 {
				last := x[len(x)-1]
				p.wantSpace = !space[last]
			}
			_, p.err = io.WriteString(p.w, x)
			p.curLine += strings.Count(x, "\n")
		case Comment:
			p.wantSpace = true
			_, p.err = fmt.Fprint(p.w, HASH, x.Text)
		case Token:
			p.wantSpace = !contiguousRight[x]
			_, p.err = fmt.Fprint(p.w, x)
		case Node:
			p.node(x)
		}
	}
}

func (p *printer) spaced(a ...interface{}) {
	for _, v := range a {
		if v == nil {
			continue
		}
		if t, ok := v.(Token); ok && contiguousLeft[t] {
		} else if p.wantSpace {
			p.space(' ')
		}
		p.nonSpaced(v)
	}
}

func (p *printer) indent() {
	for i := 0; i < p.level; i++ {
		p.space('\t')
	}
}

func (p *printer) separate(pos Pos, fallback bool) {
	p.commentsUpTo(pos.Line)
	if p.curLine > 0 && pos.Line > p.curLine {
		p.space('\n')
		if pos.Line > p.curLine+1 {
			// preserve single empty lines
			p.space('\n')
		}
		p.indent()
	} else if fallback {
		p.nonSpaced(SEMICOLON)
	}
	p.curLine = pos.Line
}

func (p *printer) sepSemicolon(v interface{}, pos Pos) {
	p.level++
	p.commentsUpTo(pos.Line)
	p.level--
	p.separate(pos, true)
	p.spaced(v)
}

func (p *printer) sepNewline(v interface{}, pos Pos) {
	p.separate(pos, false)
	p.spaced(v)
}

func (p *printer) commentsUpTo(line int) {
	if len(p.comments) < 1 {
		return
	}
	c := p.comments[0]
	if line > 0 && c.Hash.Line >= line {
		return
	}
	p.separate(c.Hash, false)
	p.spaced(c)
	p.comments = p.comments[1:]
	p.commentsUpTo(line)
}

func (p *printer) node(n Node) {
	p.stack = append(p.stack, n)
	switch x := n.(type) {
	case File:
		p.progStmts(x.Stmts)
		p.commentsUpTo(0)
		p.space('\n')
	case Stmt:
		if x.Negated {
			p.spaced(NOT)
		}
		for _, a := range x.Assigns {
			p.spaced(a)
		}
		p.spaced(x.Node)
		for _, r := range x.Redirs {
			p.spaced(r.N)
			p.nonSpaced(r.Op, r.Word)
		}
		for _, r := range x.Redirs {
			if r.Op == SHL || r.Op == DHEREDOC {
				p.space('\n')
				p.curLine++
				p.nonSpaced(r.Hdoc, wordStr(unquote(r.Word)))
			}
		}
		if x.Background {
			p.spaced(AND)
		}
	case Assign:
		if x.Name != nil {
			p.spaced(x.Name)
			if x.Append {
				p.nonSpaced(ADD_ASSIGN)
			} else {
				p.nonSpaced(ASSIGN)
			}
		}
		p.nonSpaced(x.Value)
	case Command:
		p.wordJoin(x.Args, true)
	case Subshell:
		p.spaced(LPAREN)
		if len(x.Stmts) == 0 {
			// avoid conflict with ()
			p.space(' ')
		}
		p.stmtJoin(x.Stmts)
		p.sepNewline(RPAREN, x.Rparen)
	case Block:
		p.spaced(LBRACE)
		p.stmtJoin(x.Stmts)
		p.sepSemicolon(RBRACE, x.Rbrace)
	case IfStmt:
		p.spaced(IF, x.Cond, SEMICOLON, THEN)
		p.curLine = x.Then.Line
		p.stmtJoin(x.ThenStmts)
		for _, el := range x.Elifs {
			p.sepSemicolon(ELIF, el.Elif)
			p.spaced(el.Cond, SEMICOLON, THEN)
			p.stmtJoin(el.ThenStmts)
		}
		if len(x.ElseStmts) > 0 {
			p.sepSemicolon(ELSE, x.Else)
			p.stmtJoin(x.ElseStmts)
		}
		p.sepSemicolon(FI, x.Fi)
	case StmtCond:
		p.stmtJoin(x.Stmts)
	case CStyleCond:
		p.spaced(DLPAREN, x.Cond, DRPAREN)
	case WhileStmt:
		p.spaced(WHILE, x.Cond, SEMICOLON, DO)
		p.curLine = x.Do.Line
		p.stmtJoin(x.DoStmts)
		p.sepSemicolon(DONE, x.Done)
	case UntilStmt:
		p.spaced(UNTIL, x.Cond, SEMICOLON, DO)
		p.curLine = x.Do.Line
		p.stmtJoin(x.DoStmts)
		p.sepSemicolon(DONE, x.Done)
	case ForStmt:
		p.spaced(FOR, x.Cond, SEMICOLON, DO)
		p.curLine = x.Do.Line
		p.stmtJoin(x.DoStmts)
		p.sepSemicolon(DONE, x.Done)
	case WordIter:
		p.spaced(x.Name)
		if len(x.List) > 0 {
			p.spaced(IN)
			p.wordJoin(x.List, false)
		}
	case CStyleLoop:
		p.spaced(DLPAREN, x.Init, SEMICOLON, x.Cond,
			SEMICOLON, x.Post, DRPAREN)
	case UnaryExpr:
		if x.Post {
			p.nonSpaced(x.X, x.Op)
		} else {
			p.nonSpaced(x.Op)
			p.wantSpace = false
			p.nonSpaced(x.X)
		}
	case BinaryExpr:
		switch {
		case p.compactArithm():
			p.nonSpaced(x.X, x.Op, x.Y)
		case p.inArithm():
			p.spaced(x.X, x.Op, x.Y)
		default:
			p.spaced(x.X, x.Op)
			if !p.nestedBinary() {
				p.level++
			}
			p.separate(x.Y.Pos(), false)
			p.nonSpaced(x.Y)
			if !p.nestedBinary() {
				p.level--
			}
		}
	case FuncDecl:
		if x.BashStyle {
			p.spaced(FUNCTION)
		}
		p.spaced(x.Name)
		if !x.BashStyle {
			p.nonSpaced(LPAREN, RPAREN)
		}
		p.spaced(x.Body)
	case Word:
		for _, n := range x.Parts {
			p.nonSpaced(n)
		}
	case Lit:
		p.nonSpaced(x.Value)
	case SglQuoted:
		p.nonSpaced(SQUOTE, x.Value, SQUOTE)
	case Quoted:
		p.nonSpaced(x.Quote)
		for _, n := range x.Parts {
			p.nonSpaced(n)
		}
		p.nonSpaced(quotedStop(x.Quote))
	case CmdSubst:
		if x.Backquotes {
			p.nonSpaced(BQUOTE)
		} else {
			p.nonSpaced(DOLLPR)
		}
		p.stmtJoin(x.Stmts)
		if x.Backquotes {
			p.sepNewline(BQUOTE, x.Right)
		} else {
			p.sepNewline(RPAREN, x.Right)
		}
	case ParamExp:
		if x.Short {
			p.nonSpaced(DOLLAR, x.Param)
			break
		}
		p.nonSpaced(DOLLBR)
		if x.Length {
			p.nonSpaced(HASH)
		}
		p.nonSpaced(x.Param)
		if x.Ind != nil {
			p.nonSpaced(LBRACK, x.Ind.Word, RBRACK)
		}
		if x.Repl != nil {
			if x.Repl.All {
				p.nonSpaced(QUO)
			}
			p.nonSpaced(QUO, x.Repl.Orig, QUO, x.Repl.With)
		} else if x.Exp != nil {
			p.nonSpaced(x.Exp.Op, x.Exp.Word)
		}
		p.nonSpaced(RBRACE)
	case ArithmExpr:
		p.nonSpaced(DOLLDP, x.X, DRPAREN)
	case ParenExpr:
		p.nonSpaced(LPAREN, x.X, RPAREN)
	case CaseStmt:
		p.spaced(CASE, x.Word, IN)
		for _, pl := range x.List {
			p.separate(wordFirstPos(pl.Patterns), false)
			for i, w := range pl.Patterns {
				if i > 0 {
					p.spaced(OR)
				}
				p.spaced(w)
			}
			p.nonSpaced(RPAREN)
			p.stmtJoin(pl.Stmts)
			p.level++
			p.sepNewline(DSEMICOLON, pl.Dsemi)
			p.level--
		}
		if len(x.List) == 0 {
			p.sepSemicolon(ESAC, x.Esac)
		} else {
			p.sepNewline(ESAC, x.Esac)
		}
	case DeclStmt:
		if x.Local {
			p.spaced(LOCAL)
		} else {
			p.spaced(DECLARE)
		}
		for _, w := range x.Opts {
			p.spaced(w)
		}
		for _, a := range x.Assigns {
			p.spaced(a)
		}
	case ArrayExpr:
		p.nonSpaced(LPAREN)
		p.wordJoin(x.List, false)
		p.nonSpaced(RPAREN)
	case CmdInput:
		// avoid conflict with <<
		p.spaced(CMDIN)
		p.stmtJoin(x.Stmts)
		p.nonSpaced(RPAREN)
	case EvalStmt:
		p.spaced(EVAL, x.Stmt)
	case LetStmt:
		p.spaced(LET)
		for _, n := range x.Exprs {
			p.spaced(n)
		}
	}
	p.stack = p.stack[:len(p.stack)-1]
}

func (p *printer) wordJoin(ws []Word, keepNewlines bool) {
	anyNewline := false
	for _, w := range ws {
		if keepNewlines && w.Pos().Line > p.curLine {
			if !p.inBinary() {
				p.spaced("\\")
			}
			p.nonSpaced("\n")
			if !anyNewline {
				p.level++
				anyNewline = true
			}
			p.indent()
		}
		p.spaced(w)
	}
	if anyNewline {
		p.level--
	}
}

func (p *printer) progStmts(stmts []Stmt) {
	for i, s := range stmts {
		p.separate(s.Pos(), i > 0)
		p.node(s)
	}
}

func (p *printer) stmtJoin(stmts []Stmt) {
	p.level++
	for i, s := range stmts {
		p.separate(s.Pos(), i > 0)
		p.node(s)
	}
	p.level--
}