package cleaner

import (
	"regexp"
	"sort"
	"strings"
)

var abbreviationMap = map[string]string{
	"CT": "C.T.", "MRI": "M.R.I.", "CSF": "C.S.F.", "CNN": "C.N.N.",
	"DNN": "D.N.N.", "RNN": "R.N.N.", "GAN": "G.A.N.", "AUC": "A.U.C.",
	"ROC": "R.O.C.", "ICU": "I.C.U.", "IoU": "I.o.U.", "GPU": "G.P.U.",
	"CPU": "C.P.U.", "EEG": "E.E.G.", "ECG": "E.C.G.", "DICOM": "DICOM",
	"PACS": "PACS", "AI": "A.I.", "ML": "M.L.", "DL": "D.L.", "NLP": "N.L.P.",
	"LSTM": "L.S.T.M.", "CAM": "C.A.M.", "ResNet": "ResNet", "VGG": "V.G.G.",
	"BERT": "BERT", "IEEE": "I.E.E.E.", "HIPAA": "HIPAA", "PHI": "P.H.I.",
	"TPU": "T.P.U.", "RAM": "RAM", "FPS": "F.P.S.", "RGB": "R.G.B.",
	"vs": "versus", "vs.": "versus", "w.r.t.": "with respect to",
	"i.e.": "that is,", "e.g.": "for example,", "et al.": "and others",
	"etc.": "etcetera", "Fig.": "Figure", "fig.": "figure", "Figs.": "Figures",
	"figs.": "figures", "Tab.": "Table", "tab.": "table", "Eq.": "Equation",
	"eq.": "equation", "Eqs.": "Equations", "eqs.": "equations", "Ref.": "Reference",
	"ref.": "reference", "Sec.": "Section", "sec.": "section", "Sect.": "Section",
	"approx.": "approximately",
}

var (
	hyphenBreakRE = regexp.MustCompile(`([\pL\pN_]+)-\s*\n\s*([\pL\pN_]+)`)
	dimensionRE   = regexp.MustCompile(`(\d+)\s*[×xX]\s*(\d+)`)
	pValueRE      = regexp.MustCompile(`(?i)\bp\s*([<>≤≥])\s*([\d.]+)`)
	fScoreRE      = regexp.MustCompile(`(?i)\bF(\d)[\-\s]?score`)
	percentRE     = regexp.MustCompile(`(\d+(?:\.\d+)?)\s*%`)
	plusMinusRE   = regexp.MustCompile(`(\d+(?:\.\d+)?)\s*±\s*(\d+(?:\.\d+)?)`)
	comparisonRE  = regexp.MustCompile(`(\d+(?:\.\d+)?)\s*([≤≥><])\s*(\d+(?:\.\d+)?)`)
	dashRE        = regexp.MustCompile(`([\pL\pN_])\s*[–—]\s*([\pL\pN_])`)
	multiSpaceRE  = regexp.MustCompile(`[ \t]{2,}`)
)

var comparisonWords = map[string]string{
	"<": "less than", ">": "greater than",
	"≤": "less than or equal to", "≥": "greater than or equal to",
}

// Verbalize expands common scientific notation before TTS synthesis.
func Verbalize(text string) string {
	if text == "" {
		return text
	}
	text = hyphenBreakRE.ReplaceAllString(text, `$1$2`)
	text = dimensionRE.ReplaceAllString(text, `$1 by $2`)
	text = pValueRE.ReplaceAllStringFunc(text, replaceComparisonExpression)
	text = fScoreRE.ReplaceAllString(text, `F$1 score`)
	text = plusMinusRE.ReplaceAllString(text, `$1 plus or minus $2`)
	text = comparisonRE.ReplaceAllStringFunc(text, replaceComparisonExpression)
	text = percentRE.ReplaceAllString(text, `$1 percent`)
	text = replaceAbbreviations(text)
	text = dashRE.ReplaceAllString(text, `$1, $2`)
	text = multiSpaceRE.ReplaceAllString(text, " ")
	return strings.TrimSpace(text)
}

func replaceComparisonExpression(match string) string {
	var re *regexp.Regexp
	if pValueRE.MatchString(match) {
		re = pValueRE
	} else {
		re = comparisonRE
	}
	parts := re.FindStringSubmatch(match)
	if len(parts) != 4 && len(parts) != 3 {
		return match
	}
	if re == pValueRE {
		return "p " + comparisonWords[parts[1]] + " " + parts[2]
	}
	return parts[1] + " " + comparisonWords[parts[2]] + " " + parts[3]
}

func replaceAbbreviations(text string) string {
	keys := make([]string, 0, len(abbreviationMap))
	for key := range abbreviationMap {
		keys = append(keys, key)
	}
	sort.Slice(keys, func(i, j int) bool {
		if len(keys[i]) == len(keys[j]) {
			return keys[i] < keys[j]
		}
		return len(keys[i]) > len(keys[j])
	})
	for _, key := range keys {
		pattern := regexp.MustCompile(`(^|[\s(\[])` + regexp.QuoteMeta(key) + `($|[\s,.:;!?)\]\-])`)
		replacement := `${1}` + abbreviationMap[key] + `${2}`
		text = pattern.ReplaceAllString(text, replacement)
	}
	return text
}
